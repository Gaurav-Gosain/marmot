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

const devirtSrc = `package p

type W interface{ Write([]byte) (int, error) }

type A struct{ n int }

func (a *A) Write(b []byte) (int, error) { return len(b), nil }

type B struct{}

func (B) Write(b []byte) (int, error) { return 0, nil }

var g W

func newA() W                { return &A{} }
func newAorNil(c bool) W     { if c { return nil }; return &A{} }
func newAorB(c bool) W       { if c { return B{} }; return &A{} }
func newNested() W           { return newA() }
func newRec(n int) W         { if n == 0 { return &A{} }; return newRec(n - 1) }
func newPair() (W, error)    { return &A{}, nil }

func useA() W                { return newA() }
func useAorNil() W           { return newAorNil(true) }
func useAorB() W             { return newAorB(true) }
func useNested() W           { return newNested() }
func useRec() W              { return newRec(3) }
func usePair() W             { w, _ := newPair(); return w }
func useValue() W            { return B{} }
func useParam(w W) W         { return w }
func useGlobal() W           { return g }
func usePhi(c bool) W        { w := newA(); if c { w = newA() }; return w }
func usePhiMixed(c bool) W   { w := newA(); if c { w = B{} }; return w }
`

// dynType: the one concrete dynamic type of an interface value, or nil.
func TestDynType(t *testing.T) {
	save := optDevirt1
	optDevirt1 = true
	defer func() { optDevirt1 = save }()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", devirtSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset,
		types.NewPackage("p", ""), []*ast.File{f}, ssa.SanityCheckFunctions)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"useA": "*p.A", "useAorNil": "*p.A", "useNested": "*p.A", "useRec": "", "usePair": "*p.A",
		"useValue": "p.B", "usePhi": "*p.A",
		"useAorB": "", "useParam": "", "useGlobal": "", "usePhiMixed": "",
	}
	p := &Program{prog: pkg.Prog}
	for name, w := range want {
		fn := pkg.Func(name)
		var ret *ssa.Return
		for _, b := range fn.Blocks {
			if r, ok := b.Instrs[len(b.Instrs)-1].(*ssa.Return); ok {
				ret = r
			}
		}
		got := ""
		if ty := p.dynType(ret.Results[0]); ty != nil {
			got = ty.String()
		}
		if got != w {
			t.Errorf("%s: dynamic type %q, want %q", name, got, w)
		}
	}
}
