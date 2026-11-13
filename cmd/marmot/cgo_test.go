package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCgoShow(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MARMOT_ROOT", root)
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module cgoshow\n\ngo 1.24\n")
	write("main.go", `package main

/*
#cgo LDFLAGS: -lm
#include <errno.h>
#include <unistd.h>
typedef struct { int x; double y; } pt;
static int add(int a, int b) { return a + b; }
static int nap(int us) { return usleep(us); }
static int fail(void) { errno = 2; return -1; }
extern int goTwice(int);
static int callGo(int v) { return goTwice(v); }
*/
import "C"

import "fmt"

//export goTwice
func goTwice(v C.int) C.int { return 2 * v }

func main() {
	var p C.pt
	_, err := C.fail()
	fmt.Println(C.add(1, 2), C.nap(1), C.callGo(4), p, err, C.GoString(nil))
}
`)
	r, goSrc, cSrc, err := cgoShow(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]string{}
	for _, f := range r.Functions {
		k := f.Name
		if f.Errno {
			k += " errno"
		}
		calls[k] = f.Call
	}
	for name, want := range map[string]string{"C.add": "fast", "C.nap": "bracketed", "C.callGo": "bracketed", "C.fail errno": "bracketed", "C.GoString": "helper"} {
		if calls[name] != want {
			t.Errorf("%s: %q, want %q (all: %v)", name, calls[name], want, calls)
		}
	}
	for _, f := range r.Functions {
		if f.Name == "C.add" && f.Proto != "int add(int p0, int p1)" {
			t.Errorf("C.add prototype %q", f.Proto)
		}
	}
	if len(r.Exports) != 1 || !strings.Contains(r.Exports[0], "goTwice(int p0)") {
		t.Errorf("exports %q", r.Exports)
	}
	if !strings.Contains(strings.Join(r.Types, "\n"), "C.pt = struct { x C.int; y C.double }") {
		t.Errorf("types %q", r.Types)
	}
	if strings.Join(r.LDFLAGS, " ") != "-lm" {
		t.Errorf("LDFLAGS %q", r.LDFLAGS)
	}
	for _, f := range r.CFLAGS {
		if strings.Contains(f, dir) {
			t.Errorf("CFLAGS keep cgo's own include %q", f)
		}
	}
	if !strings.Contains(goSrc, "The C calls of this file") || !strings.Contains(goSrc, "func _Cfunc_add(") {
		t.Error("-go lacks the C call declarations")
	}
	if !strings.Contains(cSrc, "Cfunc_add(") {
		t.Error("-c lacks the wrappers")
	}
}
