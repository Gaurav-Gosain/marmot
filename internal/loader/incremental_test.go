package loader

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
)

// TestIncremental loads a small module twice: the second load imports the
// unchanged packages from export data, and an edit to a leaf body leaves its
// importers' keys unchanged.
func TestIncremental(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "lib", "src")); err != nil {
		t.Skip("needs the repository")
	}
	dir := t.TempDir()
	write := func(name, src string) {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module incr\n\ngo 1.24\n")
	write("a/a.go", "package a\n\nfunc Double(x int) int { return 2 * x }\n")
	write("g/g.go", "package g\n\nfunc Map[T any](xs []T, f func(T) T) []T { for i := range xs { xs[i] = f(xs[i]) }; return xs }\n")
	write("main.go", "package main\n\nimport (\n\t\"incr/a\"\n\t\"incr/g\"\n)\n\nfunc main() { println(g.Map([]int{1}, a.Double)[0]) }\n")
	cache := t.TempDir()
	load := func() *Program {
		lp, err := Load(Config{Dir: dir, Root: root, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
			Patterns: []string{"."}, Incremental: true, CacheDir: cache,
			NeedSource: func(p *packages.Package, key string) bool { return p.Name == "main" }})
		if err != nil {
			t.Fatal(err)
		}
		return lp
	}
	first := load()
	for path, ip := range first.Incremental {
		t.Logf("first: %s src=%v", path, ip.FromSource)
	}
	if ip := first.Incremental["incr/a"]; ip == nil || !ip.FromSource {
		t.Fatalf("incr/a not loaded from source on a cold cache: %v", ip)
	}
	second := load()
	if second.Incremental["incr/a"].FromSource {
		t.Errorf("incr/a loaded from source on the second load")
	}
	if !second.Incremental["incr/g"].FromSource {
		t.Errorf("generic package incr/g must load from source when main does")
	}
	if !second.Incremental["incr"].FromSource {
		t.Errorf("main must load from source (NeedSource)")
	}
	if second.Incremental["runtime"].FromSource {
		t.Errorf("runtime loaded from source on the second load")
	}
	keyMain := second.Incremental["incr"].Key
	// Edit the body of a leaf: its key changes, its export hash does not,
	// so main keeps its key.
	time.Sleep(10 * time.Millisecond)
	write("a/a.go", "package a\n\nfunc Double(x int) int { return x + x }\n")
	third := load()
	if !third.Incremental["incr/a"].FromSource {
		t.Errorf("edited incr/a not loaded from source")
	}
	if third.Incremental["incr"].Key != keyMain {
		t.Errorf("body edit of incr/a changed the key of main")
	}
	if third.SSA == nil || third.SSAPkgs == nil {
		t.Fatal("no SSA")
	}
}
