package cgo

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestPrepare(t *testing.T) {
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("no clang")
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Skip("no go")
	}
	cfg := Config{CC: cc, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		RealGOROOT: strings.TrimSpace(string(out)), CacheDir: t.TempDir()}
	p, err := Prepare(cfg, "testdata/p", "example.com/p", nil)
	if err != nil {
		t.Fatal(err)
	}
	gotypes, _ := os.ReadFile(filepath.Join(p.OutDir, "_cgo_gotypes.go"))
	c, _ := os.ReadFile(filepath.Join(p.OutDir, "p.cgo2.c"))
	for _, want := range []string{
		"//go:linkname _cgo_",                  // direct C ABI calls
		"_Cfunc_mk_m(p0 _Ctype_int, r *_Ctype", // struct result through an adapter
		"_Cfunc_neg(p0 int32)",                 // small integers widened
		"return r, syscall.Errno(e)",           // errno result
		"var Cvar_counter = (*_Ctype_int)(",    // C variable
		"func _Cfunc_GoString(",                // helper in Go
	} {
		if !strings.Contains(string(gotypes), want) {
			t.Errorf("_cgo_gotypes.go lacks %q", want)
		}
	}
	for _, want := range []string{
		"rt_cgo_enter();",          // bracketed
		"rt_cgo_enter_fast();",     // spin: noescape and nocallback; strlen: libc leaf
		"rt_cgo_exit_errno(errno)", // fails
		"_Cfunc_mk_m(int p0, pair *r) { *r = ",
		"return (void *)&counter;",
	} {
		if !strings.Contains(string(c), want) {
			t.Errorf("p.cgo2.c lacks %q", want)
		}
	}
	if strings.Contains(string(gotypes), "syscall.Cgocall") || strings.Contains(string(gotypes), "//extern") {
		t.Errorf("gccgo calls left in _cgo_gotypes.go")
	}
	// Cached: a second Prepare reuses the directory.
	p2, err := Prepare(cfg, "testdata/p", "example.com/p", nil)
	if err != nil || p2.OutDir != p.OutDir {
		t.Fatalf("second Prepare: %v, %s vs %s", err, p2.OutDir, p.OutDir)
	}
}

func TestBadFlags(t *testing.T) {
	for _, tc := range []struct{ directive, want string }{
		{"CFLAGS: -fplugin=evil.so", "invalid flag"},
		{"LDFLAGS: -Wl,--plugin=evil.so", "invalid flag"},
		{"CFLAGS: -Xclang -load", "invalid flag"},
	} {
		dir := t.TempDir()
		src := "package bad\n\n/*\n#cgo " + tc.directive + "\n*/\nimport \"C\"\n"
		if err := os.WriteFile(filepath.Join(dir, "bad.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Prepare(Config{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, CacheDir: t.TempDir()}, dir, "example.com/bad", nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("#cgo %s: got %v, want %q", tc.directive, err, tc.want)
		}
	}
}

func TestExport(t *testing.T) {
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("no clang")
	}
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Skip("no go")
	}
	cfg := Config{CC: cc, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		RealGOROOT: strings.TrimSpace(string(out)), CacheDir: t.TempDir()}
	p, err := Prepare(cfg, "testdata/e", "example.com/e", nil)
	if err != nil {
		t.Fatal(err)
	}
	gotypes, _ := os.ReadFile(filepath.Join(p.OutDir, "_cgo_gotypes.go"))
	exp, _ := os.ReadFile(filepath.Join(p.OutDir, "_cgo_export.c"))
	for _, want := range []string{
		"_goAdd(p0 _Ctype_int, p1 _Ctype_int) _Ctype_int {", // C-level symbol, same types
		"_goPt(p0 *_Ctype_pt, r *_Ctype_pt) {",              // struct by pointer
		"_goNeg(p0 int32) int32 {",                          // small integers widened
		"r.r0, r.r1 = goTwo()",                              // several results
	} {
		if !strings.Contains(string(gotypes), want) {
			t.Errorf("_cgo_gotypes.go lacks %q", want)
		}
	}
	for _, want := range []string{
		"int32_t _cgo_tok = rt_cgo_callback_enter();",
		"rt_cgo_callback_exit(_cgo_tok);",
		"_goPt(&p0, &r);",
	} {
		if !strings.Contains(string(exp), want) {
			t.Errorf("_cgo_export.c lacks %q", want)
		}
	}
	for _, bad := range []string{"Cgoexp_", "__asm__", "runtime_iscgo"} {
		if strings.Contains(string(exp), bad) || bad == "Cgoexp_" && strings.Contains(string(gotypes), bad) {
			t.Errorf("gccgo leftover %q", bad)
		}
	}
	if !slices.Contains(p.CFiles, filepath.Join(p.OutDir, "_cgo_export.c")) {
		t.Errorf("_cgo_export.c is not compiled: %v", p.CFiles)
	}
}
