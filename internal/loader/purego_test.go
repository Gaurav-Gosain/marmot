package loader

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestPurego loads a program using purego (the module cache's v0.11.3,
// copied as a local replacement) and checks the edits: purego's marmot
// side is there, fakecgo is empty, a RegisterLibFunc call with a func
// variable gets an adapter and one with an interface does not.
func TestPurego(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "lib", "src")); err != nil {
		t.Skip("needs the repository")
	}
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Skip(err)
	}
	src := filepath.Join(strings.TrimSpace(string(out)), "github.com", "ebitengine", "purego@v0.11.3")
	if _, err := os.Stat(src); err != nil {
		t.Skip("purego v0.11.3 is not in the module cache")
	}
	dir := t.TempDir()
	if out, err := exec.Command("cp", "-R", src, filepath.Join(dir, "purego")).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	_ = exec.Command("chmod", "-R", "u+w", filepath.Join(dir, "purego")).Run()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module pg\n\ngo 1.25.0\n\nrequire github.com/ebitengine/purego v0.11.3\n\nreplace github.com/ebitengine/purego => ./purego\n")
	write("main.go", `package main

import "github.com/ebitengine/purego"

type point struct{ X, Y float64 }

var strlen func(string) uintptr
var dist func(point, *point) (d float64)

func main() {
	var any_ any = &strlen
	purego.RegisterLibFunc(&strlen, 0, "strlen")
	purego.RegisterLibFunc(
		&dist,
		0,
		"dist",
	)
	purego.RegisterFunc(any_, 0)
}
`)
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
		byPath := map[string]*packages.Package{}
		for _, p := range lp.All {
			byPath[p.PkgPath] = p
		}
		pg, mainPkg := byPath[puregoPath], byPath["pg"]
		if pg == nil || mainPkg == nil {
			t.Fatalf("incremental=%v: purego or main not loaded", incr)
		}
		for _, name := range []string{"RegisterFuncMarmot", "RegisterLibFuncMarmot", "marmotDlopen", "callbackasmAddrGC"} {
			if pg.Types.Scope().Lookup(name) == nil {
				t.Errorf("incremental=%v: purego.%s missing", incr, name)
			}
		}
		if fc := byPath[fakecgoPath]; fc != nil && len(fc.Types.Scope().Names()) != 0 {
			t.Errorf("incremental=%v: fakecgo not empty: %v", incr, fc.Types.Scope().Names())
		}
		sp := lp.SSAPkgs[mainPkg.Types]
		for name, want := range map[string]bool{puregoAdapter + "1": true, puregoAdapter + "2": true, puregoAdapter + "3": false} {
			if got := sp.Func(name) != nil; got != want {
				t.Errorf("incremental=%v: %s defined: %v, want %v", incr, name, got, want)
			}
		}
	}
}

func TestPuregoVersions(t *testing.T) {
	p := &packages.Package{Module: &packages.Module{Path: puregoPath, Version: "v0.9.1"}}
	if err := puregoVersion(p); err == nil || !strings.Contains(err.Error(), "v0.10.x, v0.11.x") {
		t.Errorf("v0.9.1: %v", err)
	}
	p.Module.Version = "v0.11.0-alpha.1"
	if err := puregoVersion(p); err != nil {
		t.Errorf("v0.11.0-alpha.1: %v", err)
	}
	if _, err := puregoFileEdits("func.go", "package purego\n"); err == nil || !strings.Contains(err.Error(), "do not match this version") {
		t.Errorf("a changed func.go: %v", err)
	}
	if e, err := puregoFileEdits("cdecl.go", "package purego\n"); err != nil || e != nil {
		t.Errorf("an unedited file: %v %v", e, err)
	}
}

// Every patched file of every purego release in the module cache takes
// the edits (v0.10.x keeps the callbacks in syscall_sysv.go and has no
// goarmsoftfp; v0.11.x moved them to syscall_unix.go).
func TestPuregoPatchesAllVersions(t *testing.T) {
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Skip(err)
	}
	seen := 0
	for _, v := range []string{"v0.10.0", "v0.10.2", "v0.11.1", "v0.11.3"} {
		dir := filepath.Join(strings.TrimSpace(string(out)), "github.com", "ebitengine", "purego@"+v)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		seen++
		files := 0
		for base := range puregoPatches {
			src, err := os.ReadFile(filepath.Join(dir, base))
			if err != nil {
				continue
			}
			files++
			if _, err := puregoFileEdits(base, string(src)); err != nil {
				t.Errorf("%s: %v", v, err)
			}
		}
		if files != 4 {
			t.Errorf("%s: %d patched files, want 4 (dlfcn, func, go_runtime and one callback file)", v, files)
		}
	}
	if seen == 0 {
		t.Skip("no purego in the module cache")
	}
}
