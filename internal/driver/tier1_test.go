package driver

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A dev build after background tier 1 links the runner's -O2 objects in
// place of the tier0 ones. Their pc table must still name the source: the
// traceback of a panic gives main.go and the line, as it does in the first
// build. (The runner's fragments were once empty, so every repeat dev build
// printed tracebacks without a file or a line.)
func TestTier1Traceback(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program twice and runs tier 1")
	}
	inst := os.Getenv("MARMOT_ROOT") // a checkout with an installed build (a worktree has none)
	if inst == "" {
		r, err := filepath.Abs("../..")
		if err != nil {
			t.Fatal(err)
		}
		inst = r
	}
	if _, err := os.Stat(filepath.Join(inst, "build", "bin", "marmot-backend")); err != nil {
		t.Skip("no installed backend")
	}
	dir := t.TempDir()
	// A root of its own (the installed tools, an empty cache): tier 1 then
	// has every module to compile, and the test leaves no objects behind.
	root := filepath.Join(dir, "root")
	for _, p := range []string{"build/bin", "build/lib", "lib"} {
		dst := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(inst, filepath.FromSlash(p)), dst); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(dir, "prog")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"go.mod":  "module example.com/tier1\n\ngo 1.24\n",
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\txs := []int{1, 2}\n\tfor i := 0; i <= len(xs); i++ {\n\t\tfmt.Println(xs[i])\n\t}\n}\n",
	} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MARMOT_TIER1", "sync") // the build waits for the runner
	for _, n := range []string{"first", "second"} {
		out := filepath.Join(dir, n)
		cfg := Config{Root: root, Dir: src, Output: out, OptLevel: "2", Emit: "exe", GC: "precise", Dialect: "llvm",
			Patterns: []string{"."}, Stderr: os.Stderr}
		if _, err := Build(cfg); err != nil {
			t.Fatal(err)
		}
		if n == "first" {
			if _, err := os.Stat(filepath.Join(root, "build", "cache", "tier1", "epoch")); err != nil {
				t.Fatalf("tier 1 cached no objects after the first build: %v", err)
			}
		}
		cmd := exec.Command(out)
		cmd.Env = append(os.Environ(), "MARMOT_OUTPUT=plain")
		msg, _ := cmd.CombinedOutput()
		if !bytes.Contains(msg, []byte("main.go:8")) {
			t.Errorf("%s build: the traceback does not name main.go:8:\n%s", n, msg)
		}
	}
}
