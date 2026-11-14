// Package gotest checks marmot test against go test: the same packages,
// the same verbose output once timings are removed.
//
// go test ./tests/gotest uses the marmot in build/bin (or $MARMOT_BIN).
package gotest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestMainPackage tests a package main with internal tests, an example and
// an external test package. The generated test main is package main too:
// the package under test must keep its import path in symbols, or main.main
// and main's init guard are defined twice.
func TestMainPackage(t *testing.T) {
	bin, repo := marmotBin(t)
	dir := t.TempDir()
	copyDir(t, filepath.Join("testdata", "mainpkg"), dir)
	root := privateRoot(t, repo)

	want, err := run(dir, []string{"GOFLAGS=-count=1"}, "go", "test", "-v", ".")
	if err != nil {
		t.Fatalf("go test: %v\n%s", err, want)
	}
	got, err := run(dir, []string{"MARMOT_ROOT=" + root, "MARMOT_DAEMON=0", "MARMOT_TIER1=0", "MARMOT_OUTPUT=plain", "MARMOT_CACHE_MAX_MB=1024"}, bin, "test", "-v", ".")
	if err != nil {
		t.Fatalf("marmot test: %v\n%s", err, got)
	}
	if g, w := normalize(got), normalize(want); g != w {
		t.Errorf("marmot test -v differs from go test -v\n--- marmot\n%s\n--- go\n%s", g, w)
	}
}

var (
	durations = regexp.MustCompile(`\(\d+\.\d+s\)`)
	okLine    = regexp.MustCompile(`(?m)^ok\s+\S+\s+.*$`)
)

// normalize drops what may differ between runs and tools: durations and
// the final ok line (go prints the import path, marmot the directory).
func normalize(s string) string {
	s = durations.ReplaceAllString(s, "(T)")
	s = okLine.ReplaceAllString(s, "ok")
	return strings.TrimSpace(s)
}

func marmotBin(t *testing.T) (bin, repo string) {
	repo, _ = filepath.Abs("../..")
	bin = os.Getenv("MARMOT_BIN")
	if bin == "" {
		bin = filepath.Join(repo, "build", "bin", "marmot")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("no marmot at %s", bin)
	}
	if _, err := os.Stat(filepath.Join(repo, "build", "bin", "marmot-backend")); err != nil {
		t.Skip("no build/bin/marmot-backend")
	}
	return bin, repo
}

// privateRoot is a marmot root with its own cache; the tools and lib come
// from the repository.
func privateRoot(t *testing.T, repo string) string {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"lib", "build/bin", "build/lib"} {
		if err := os.Symlink(filepath.Join(repo, l), filepath.Join(root, l)); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func run(dir string, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	return b.String(), err
}

func copyDir(t *testing.T, from, to string) {
	ents, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
