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

const roconvSrc = `package p

import "unsafe"

var g []byte
var gp *byte

func sum(b []byte) (n int)         { for _, c := range b { n += int(c) }; return }
func index(b []byte, i int) byte    { return b[i] }
func lenOnly(b []byte) int          { return len(b) + cap(b) }
func reslice(b []byte) int          { return sum(b[1:]) }
func copyFrom(dst, b []byte) int    { return copy(dst, b) }
func appendFrom(dst, b []byte) []byte { return append(dst, b...) }
func toString(b []byte) string      { return string(b) }
func isNil(b []byte) bool           { return b == nil }
func phi(b []byte, c bool) int      { x := b; if c { x = b[2:] }; return sum(x) }
func arrayRead(b []byte) byte       { return (*[2]byte)(b)[1] }

func store(b []byte)                { b[0] = 1 }
func copyInto(b, src []byte)        { copy(b, src) }
func appendInto(b []byte) []byte    { return append(b[:0], 'x') }
func clearIt(b []byte)              { clear(b) }
func passToWriter(b []byte)         { store(b) }
func keep(b []byte)                 { g = b }
func elemAddr(b []byte)             { gp = &b[0] }
func arrayWrite(b []byte)           { (*[2]byte)(b)[1] = 0 }
func unsafeData(b []byte) *byte     { return unsafe.SliceData(b) }
func goWriter(b []byte)             { go store(b) }
func deferWriter(b []byte)          { defer store(b) }
func viaIface(b []byte, w interface{ Write([]byte) (int, error) }) { w.Write(b) }
func viaFunc(b []byte, f func([]byte)) { f(b) }
func boxed(b []byte) any            { return b }
func recursive(b []byte, n int) int { if n == 0 { return 0 }; return recursive(b, n-1) }
func phiWrite(b []byte, c bool)     { x := b; if c { x = b[2:] }; x[0] = 1 }
func notSlice(n int) int            { return n }
func genRead[B ~[]byte | ~string](b B) int { return len(b) }
func genWrite[T any](b []T, v T)    { b[0] = v }
func useGen(b []byte) int           { genWrite(b, 1); return genRead(b) }
`

// paramWritten: whether a function may write the elements of its first
// parameter (every unknown counts as a write).
func TestParamWritten(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", roconvSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset,
		types.NewPackage("p", ""), []*ast.File{f}, ssa.SanityCheckFunctions|ssa.InstantiateGenerics)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"sum": false, "index": false, "lenOnly": false, "reslice": false, "toString": false,
		"isNil": false, "phi": false, "arrayRead": false,
		"store": true, "appendInto": true, "clearIt": true, "passToWriter": true, "keep": true,
		"elemAddr": true, "arrayWrite": true, "unsafeData": true, "goWriter": true, "deferWriter": true,
		"viaIface": true, "viaFunc": true, "boxed": true, "recursive": true, "phiWrite": true, "notSlice": true,
	}
	// The second parameter of copyFrom and appendFrom is only read; the first
	// of copyInto is written.
	second := map[string]bool{"copyFrom": false, "appendFrom": false}
	p := &Program{}
	for name, w := range want {
		fn := pkg.Func(name)
		if fn == nil {
			t.Fatalf("no function %s", name)
		}
		if got := p.paramWritten(fn, 0); got != w {
			t.Errorf("%s: written = %v, want %v", name, got, w)
		}
	}
	for name, w := range second {
		if got := p.paramWritten(pkg.Func(name), 1); got != w {
			t.Errorf("%s (second parameter): written = %v, want %v", name, got, w)
		}
	}
	// Instances are summarized on their own bodies; generic bodies are unknown.
	inst := 0
	for fn := range ssautil.AllFunctions(pkg.Prog) {
		switch fn.String() {
		case "p.genRead[[]byte]":
			inst++
			if p.paramWritten(fn, 0) {
				t.Errorf("%s: written, want not written", fn)
			}
		case "p.genWrite[byte]":
			inst++
			if !p.paramWritten(fn, 0) {
				t.Errorf("%s: not written, want written", fn)
			}
		}
	}
	if inst != 2 {
		t.Errorf("found %d of the 2 instances", inst)
	}
	if !p.paramWritten(pkg.Func("genRead"), 0) {
		t.Errorf("genRead (generic body): not written, want written (unknown)")
	}
	if !p.paramWritten(pkg.Func("copyInto"), 0) {
		t.Errorf("copyInto: destination counted as not written")
	}
}
