package codegen

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

const stdFuncSrc = `package lib

type T struct{ n int }

func (t T) Get() int   { return t.n }
func (t *T) Set(n int) { t.n = n }

func Plain() func() int {
	x := 1
	return func() int { return x }
}

func Gen[E any](s []E) func() int {
	return func() int { return len(s) }
}

func UseGen() int { return Gen([]int{1})() }
`

// stdFunc marks the functions, methods, closures and method wrappers of a
// standard library package, and no generic instance or closure in one.
func TestStdFunc(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "lib.go", stdFuncSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset,
		types.NewPackage("lib", ""), []*ast.File{f}, ssa.InstantiateGenerics)
	if err != nil {
		t.Fatal(err)
	}
	all := ssautil.AllFunctions(pkg.Prog)
	got := map[string]bool{}
	for fn := range all {
		if fn.Pkg != pkg && (fn.Object() == nil || fn.Object().Pkg() != pkg.Pkg) {
			continue
		}
		got[fn.String()] = true
	}
	std := &Program{opts: Options{StdPkgs: map[string]bool{"lib": true}}}
	user := &Program{opts: Options{StdPkgs: map[string]bool{"other": true}}}
	want := map[string]bool{
		"lib.Plain": true, "lib.Plain$1": true, "lib.UseGen": true,
		"(lib.T).Get": true, "(*lib.T).Set": true, "(*lib.T).Get": true, // the last one a wrapper
		"lib.Gen[int]": false, "lib.Gen[int]$1": false,
	}
	for fn := range all {
		w, ok := want[fn.String()]
		if !ok {
			continue
		}
		delete(want, fn.String())
		if g := std.stdFunc(fn); g != w {
			t.Errorf("%s: stdFunc = %v, want %v", fn, g, w)
		}
		if user.stdFunc(fn) {
			t.Errorf("%s: marked in a build where lib is not standard", fn)
		}
	}
	for name := range want {
		t.Errorf("no function %s (have %v)", name, got)
	}
}
