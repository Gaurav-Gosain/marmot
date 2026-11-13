package loader

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestAsmJump checks that a body-less function implemented in assembly as
// a jump to a Go function gets a body calling it, in full and incremental
// loads.
func TestAsmJump(t *testing.T) {
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
	write("go.mod", "module asmj\n\ngo 1.24\n")
	write("a/a.go", "package a\n\nimport _ \"asmj/b\"\n\nfunc Double(x int) int { return 2 * x }\n\nfunc Twice(x int) int\n\nfunc twice2(x int) int\n\nfunc Raw(x int) int\n\nfunc Hidden(x int) int\n\nfunc Far(x int) int\n")
	write("a/stub.s", "#include \"textflag.h\"\n\n// Go functions.\nTEXT ·Twice(SB),NOSPLIT,$0-16\n\tJMP\t·Double(SB)\n\nTEXT ·twice2(SB),NOSPLIT,$0-16\n\tJMP\tasmj∕b·Double(SB) // another package\n\nTEXT ·Hidden(SB),NOSPLIT,$0-16\n\tJMP\tasmj∕b·half(SB)\n\nTEXT ·Far(SB),NOSPLIT,$0-16\n\tJMP\tasmj∕c·Double(SB)\n\nTEXT ·Raw(SB),NOSPLIT,$0-16\n\tMOVQ\tx+0(FP), AX\n\tRET\n")
	write("b/b.go", "package b\n\nfunc Double(x int) int { return x + x }\n\nfunc half(x int) int { return x / 2 }\n")
	write("main.go", "package main\n\nimport (\n\t\"asmj/a\"\n\t_ \"asmj/b\"\n)\n\nfunc main() { println(a.Twice(1)) }\n")
	for _, incr := range []bool{false, true} {
		cfg := Config{Dir: dir, Root: root, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Patterns: []string{"."}}
		if incr {
			cfg.Incremental, cfg.CacheDir = true, t.TempDir()
			cfg.NeedSource = func(p *packages.Package, key string) bool { return true }
		}
		lp, err := Load(cfg)
		if err != nil {
			t.Fatalf("incremental=%v: %v", incr, err)
		}
		var pa *packages.Package
		for _, p := range lp.All {
			if p.PkgPath == "asmj/a" {
				pa = p
			}
		}
		if pa == nil {
			t.Fatalf("incremental=%v: asmj/a not loaded", incr)
		}
		sp := lp.SSAPkgs[pa.Types]
		for name, want := range map[string]bool{"Twice": true, "twice2": true, "Hidden": false, "Far": false, "Raw": false} {
			fn := sp.Func(name)
			if fn == nil {
				t.Fatalf("incremental=%v: %s missing", incr, name)
			}
			if got := len(fn.Blocks) > 0; got != want {
				t.Errorf("incremental=%v: %s has a body: %v, want %v", incr, name, got, want)
			}
		}
	}
}
