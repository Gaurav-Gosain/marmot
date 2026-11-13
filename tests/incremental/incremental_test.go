// Package incremental checks separate compilation (incremental dev builds)
// against clean builds. For each edit of package a in testdata/prog, an
// incremental build after the edit must print what a clean build of the
// edited tree prints, and every module of the clean build must be byte for
// byte the module the incremental build has under the same key: a cached
// module that depends on something its key does not cover (another
// package's body, positions, generic code) shows up here.
//
// go test ./tests/incremental uses the marmot in build/bin (or $MARMOT_BIN),
// with the module keys MARMOT_API_KEYS selects.
package incremental

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const base = "5 2 [3 6 9] 7 {4 y} -1 1"

var edits = []struct {
	name     string
	file     string // the file edited: in the program (default a/a.go), or lib/... in the root
	old, new string
	want     string
}{
	// A body edit that moves the lines after it: importers keep their modules.
	{"lineshift", "", "func Keep(p *int) int {\n", "func Keep(p *int) int {\n\t_ = 0\n", base},
	// Summaries change: a function starts panicking, keeping its parameter,
	// recovering.
	{"panics", "", "func MayPanic(i int) int {\n", "func MayPanic(i int) int {\n\tif i < 0 {\n\t\tpanic(\"neg\")\n\t}\n", "recovered: neg"},
	{"escapes", "", "func Keep(p *int) int {\n", "func Keep(p *int) int {\n\tsink = p\n", base},
	{"recovers", "", "func Recov() (r int) {\n\treturn 1\n", "func Recov() (r int) {\n\tdefer func() {\n\t\tif recover() != nil {\n\t\t\tr = 2\n\t\t}\n\t}()\n\tpanic(\"x\")\n", "5 2 [3 6 9] 7 {4 y} -1 2"},
	// Generic code compiled into importers: an exported generic function, a
	// promoted method of an unexported generic type, an unexported helper
	// its body calls.
	{"generic", "", "\tfor _, x := range xs {\n\t\tout = append(out, f(x))\n\t}\n", "\tfor i := len(xs) - 1; i >= 0; i-- {\n\t\tout = append(out, f(xs[i]))\n\t}\n", "5 2 [9 6 3] 7 {4 y} -1 1"},
	{"promoted", "", "func (l *list[E]) Len() int { return len(l.items) }", "func (l *list[E]) Len() int { return len(l.items) + 100 }", "5 102 [3 6 9] 7 {4 y} -1 1"},
	{"helper", "", "make([]B, 0, grow(len(xs)))", "make([]B, 0, grow(len(xs), 1))", base},
	// The API: a struct layout and a constant.
	{"layout", "", "\tX int\n\tY string\n", "\tX int\n\tZ int\n\tY string\n", "5 2 [3 6 9] 7 {4 0 y} -1 1"},
	{"const", "", "const C = 3", "const C = 4", "5 2 [4 8 12] 7 {4 y} -1 1"},
	// A new instantiation in the importer.
	{"newinst", "b/b.go", "\tt := a.T{X: 4, Y: \"y\"}\n", "\tt := a.T{X: len(a.Map([]float64{0.5}, func(f float64) float64 { return f }))*3 + 1, Y: \"y\"}\n", base},
	// The runtime, which every module depends on.
	{"runtime", "lib/src/runtime/panic.go", "func gopanic(v any) {\n", "func gopanic(v any) {\n\t_ = 0\n", base},
}

func TestEdits(t *testing.T) {
	bin, repo := marmotBin(t)
	root := privateRoot(t, repo)
	for _, e := range edits {
		t.Run(e.name, func(t *testing.T) {
			dir := t.TempDir()
			copyTree(t, "testdata/prog", dir)
			b := builder{t: t, bin: bin, root: root, dir: dir}
			if got := b.buildRun(); got != base {
				t.Fatalf("before the edit: got %q, want %q", got, base)
			}
			file := filepath.Join(dir, "a", "a.go")
			switch {
			case strings.HasPrefix(e.file, "lib/"):
				file = filepath.Join(root, e.file)
				orig, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				defer os.WriteFile(file, orig, 0o644)
			case e.file != "":
				file = filepath.Join(dir, e.file)
			}
			replace(t, file, e.old, e.new)
			if e.name == "helper" {
				// The helper's signature changes with its call.
				replace(t, filepath.Join(dir, "a", "a.go"), "func grow(n int) int { return n }", "func grow(n, m int) int { return n + m - 1 }")
			}
			incr := b.buildRun()
			if incr != e.want {
				t.Errorf("incremental build after the edit: got %q, want %q", incr, e.want)
			}
			// Clean: every package from source, every module generated again.
			mlir := filepath.Join(root, "build", "cache", "mlir")
			saved := filepath.Join(root, "build", "cache", "mlir.incr")
			os.RemoveAll(saved)
			if err := os.Rename(mlir, saved); err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(saved)
			os.RemoveAll(filepath.Join(root, "build", "cache", "types"))
			stamps, _ := filepath.Glob(filepath.Join(root, "build", "cache", "exe-*"))
			for _, s := range stamps {
				os.RemoveAll(s)
			}
			if clean := b.buildRun(); clean != incr {
				t.Errorf("clean build: got %q, incremental %q", clean, incr)
			}
			mods, _ := filepath.Glob(filepath.Join(mlir, "*.mlir"))
			if len(mods) == 0 {
				t.Fatal("the clean build wrote no modules")
			}
			for _, m := range mods {
				want, _ := os.ReadFile(m)
				got, err := os.ReadFile(filepath.Join(saved, filepath.Base(m)))
				if err != nil {
					t.Errorf("module %s of the clean build is not in the incremental cache", filepath.Base(m))
					continue
				}
				if !bytes.Equal(got, want) {
					t.Errorf("module %s differs between the incremental and the clean build", filepath.Base(m))
				}
			}
		})
	}
}

type builder struct {
	t              *testing.T
	bin, root, dir string
}

// buildRun builds the program in b.dir and returns what it prints.
func (b builder) buildRun() string {
	b.t.Helper()
	exe := filepath.Join(b.dir, "prog")
	env := []string{"MARMOT_ROOT=" + b.root, "MARMOT_DAEMON=0", "MARMOT_TIER1=0", "MARMOT_OUTPUT=plain", "MARMOT_CACHE_MAX_MB=4096", "MARMOT_INCREMENTAL=", "MARMOT_API_KEYS=" + os.Getenv("MARMOT_API_KEYS")}
	if out, err := run(b.dir, env, b.bin, "build", "-o", exe, "."); err != nil {
		b.t.Fatalf("build: %v\n%s", err, out)
	}
	out, err := run(b.dir, nil, exe)
	if err != nil {
		b.t.Fatalf("run: %v\n%s", err, out)
	}
	return strings.TrimSpace(out)
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

// privateRoot is a marmot root whose cache and lib (small; edits may change
// the runtime) are the test's own; the tools come from the repository.
func privateRoot(t *testing.T, repo string) string {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	copyTree(t, filepath.Join(repo, "lib"), filepath.Join(root, "lib"))
	for _, l := range []string{"build/bin", "build/lib"} {
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

func copyTree(t *testing.T, from, to string) {
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		dst := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func replace(t *testing.T, file, old, new string) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Count(s, old) != 1 {
		t.Fatalf("%s: %q found %d times", file, old, strings.Count(s, old))
	}
	// A new mtime even within the file system's timestamp resolution.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(file, []byte(strings.Replace(s, old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}
