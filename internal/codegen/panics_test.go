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

// The may-panic predicate shared by defer record elision and dead
// initializer removal, checked against the table in docs/verify.md: each
// function below performs one operation; mayPanicCall must say whether it
// can panic.

const panicSrc = `package p

type S struct{ a, b int }
type SI struct{ v any }
type SN struct{ s struct{ x [1]any } }

var g int
var gp *S

func add(x, y int) int          { return x + y }
func divVar(x, y int) int       { return x / y }
func remVar(x, y int) int       { return x % y }
func divConst(x int) int        { return x / 7 }
func divFloat(x, y float64) float64 { return x / y }
func shlSigned(x, n int) int    { return x << n }
func shlUnsigned(x int, n uint) int { return x << n }
func shlConst(x int) int        { return x << 3 }
func idxSlice(s []int, i int) int { return s[i] }
func idxString(s string, i int) byte { return s[i] }
func idxArrConst() int          { var a [4]int; return a[2] }
func sliceExpr(s []int) []int   { return s[1:] }
func toArray(s []int) [2]int    { return [2]int(s) }
func cmpIface(x, y any) bool    { return x == y }
func cmpStructIface(x, y SI) bool { return x == y }
func cmpArrayIface(x, y [2]any) bool { return x == y }
func cmpNested(x, y SN) bool    { return x == y }
func cmpInts(x, y S) bool       { return x == y }
func cmpStrings(x, y string) bool { return x == y }
func assert(x any) int          { return x.(int) }
func assertOK(x any) bool       { _, ok := x.(int); return ok }
func mapWrite(m map[string]int) { m["a"] = 1 }
func mapRead(m map[string]int) int { return m["a"] }
func makeSliceVar(n int) []int  { return make([]int, n) }
func makeChanVar(n int) chan int { return make(chan int, n) }
func makeMapVar(n int) map[int]int { return make(map[int]int, n) }
func derefParam(p *S) int       { return p.a }
func storeParam(p *int)         { *p = 1 }
func derefLocal() int           { s := &S{1, 2}; return s.a }
func globalLoad() int           { return g }
func globalPtrField() int       { return gp.a }
func send(c chan int)           { c <- 1 }
func recv(c chan int) int       { return <-c }
func closeChan(c chan int)      { close(c) }
func callSafe(x int) int        { return add(x, 1) }
func callUnsafe(x int) int      { return divVar(x, 0) }
func recursive(n int) int       { if n == 0 { return 0 }; return recursive(n - 1) }
func invoke(e error) string     { return e.Error() }
func dynamic(f func() int) int  { return f() }
func explicitPanic()            { panic("x") }
func builtins(s []int) int      { copy(s, s); s = append(s, s...); return len(s) + cap(s) + min(1, 2) }
func makeConst() []int          { return make([]int, 4) }
func makeHuge() []byte          { return make([]byte, 1<<62) }
func convert(x int) float64     { return float64(x) + float64(len(string(rune(x)))) }
func iface(x int) any           { return x }
func closure(x int) func() int  { return func() int { return x } }
`

func TestMayPanic(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", panicSrc, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset,
		types.NewPackage("p", ""), []*ast.File{f}, ssa.SanityCheckFunctions)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"add": false, "divVar": true, "remVar": true, "divConst": false, "divFloat": false,
		"shlSigned": true, "shlUnsigned": false, "shlConst": false,
		"idxSlice": true, "idxString": true, "idxArrConst": false, "sliceExpr": true, "toArray": true,
		"cmpIface": true, "cmpStructIface": true, "cmpArrayIface": true, "cmpNested": true,
		"cmpInts": false, "cmpStrings": false,
		"assert": true, "mapWrite": true, "mapRead": true,
		"makeSliceVar": true, "makeChanVar": true, "makeMapVar": false,
		"derefParam": true, "storeParam": true, "derefLocal": false, "globalLoad": false, "globalPtrField": true,
		"send": true, "recv": true, "closeChan": true,
		"callSafe": false, "callUnsafe": true, "recursive": true, "invoke": true, "dynamic": true,
		"explicitPanic": true, "builtins": false, "makeConst": true, "makeHuge": true, "assertOK": true, "convert": false, "iface": false, "closure": false,
	}
	p := &Program{}
	for name, w := range want {
		fn := pkg.Func(name)
		if fn == nil {
			t.Fatalf("no function %s", name)
		}
		if got := p.mayPanicCall(fn, nil, func(ssa.Value) bool { return false }); got != w {
			t.Errorf("%s: may panic = %v, want %v", name, got, w)
		}
	}
	// makeConst is new([4]int)[:] plus the slice instruction, which counts
	// as may-panic (conservative); assertOK likewise (a comma-ok assertion
	// cannot panic, but every TypeAssert counts). The Alloc of makeHuge is
	// what must count: checked directly.
	for _, b := range pkg.Func("makeHuge").Blocks {
		for _, instr := range b.Instrs {
			if a, ok := instr.(*ssa.Alloc); ok && !p.instrMayPanic(a, nil) {
				t.Errorf("makeHuge: Alloc %s counted as unable to panic", a.Type())
			}
		}
	}
	// A non-nil argument removes the nil checks of derefParam.
	if p.mayPanicCall(pkg.Func("derefParam"), []ssa.Value{pkg.Members["gp"].(*ssa.Global)}, func(v ssa.Value) bool { return true }) {
		t.Errorf("derefParam with a non-nil argument: may panic")
	}
}

// deadInitSafe decides what dead initializer removal may drop.
func TestDeadInitSafe(t *testing.T) {
	src := `package p

var unusedIdx = s[k+3]
var s = []int{1}
var k = 0
var unusedNil = np.x
var np *struct{ x int }
var n = 3
var unusedShift = 1 << n
var unusedMap = map[string]int{"a": 1}
var key any = 1
var unusedIfaceMap = map[any]int{key: 1}
var unusedMake = make([]int, 4)
var unusedBig = make([]byte, 1<<62)
var unusedVarMake = make([]int, n)
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset,
		types.NewPackage("p", ""), []*ast.File{f}, 0)
	if err != nil {
		t.Fatal(err)
	}
	p := &Program{}
	kinds := map[string][2]int{} // instruction kind -> safe, unsafe counts
	for _, b := range pkg.Func("init").Blocks {
		for _, instr := range b.Instrs {
			k := ""
			switch x := instr.(type) {
			case *ssa.IndexAddr:
				if _, arr := x.X.Type().Underlying().(*types.Pointer); arr {
					if _, c := x.Index.(*ssa.Const); c {
						k = "IndexAddr const into array"
						break
					}
				}
				k = "IndexAddr"
			case *ssa.UnOp:
				if x.Op == token.MUL {
					switch x.X.(type) {
					case *ssa.Global:
						k = "load global"
					case *ssa.FieldAddr, *ssa.IndexAddr:
						k = "load of computed address"
					default:
						k = "load through pointer"
					}
				}
			case *ssa.FieldAddr:
				k = "FieldAddr"
			case *ssa.BinOp:
				if x.Op == token.SHL {
					k = "shift"
				}
			case *ssa.MapUpdate:
				if types.IsInterface(x.Key.Type()) {
					k = "MapUpdate iface key"
				} else {
					k = "MapUpdate string key"
				}
			case *ssa.MakeSlice:
				k = "MakeSlice var"
			case *ssa.Alloc:
				if pt, ok := x.Type().Underlying().(*types.Pointer); ok && types.SizesFor("gc", "amd64").Sizeof(pt.Elem()) >= 1<<40 {
					k = "Alloc huge"
				}
			}
			if k == "" {
				continue
			}
			c := kinds[k]
			if deadInitSafe(p, instr) {
				c[0]++
			} else {
				c[1]++
			}
			kinds[k] = c
		}
	}
	wantSafe := map[string]bool{
		"load global": true, "load of computed address": true, "MapUpdate string key": true,
		"IndexAddr const into array": true,
		"IndexAddr":                  false, "FieldAddr": false, "shift": false,
		"MapUpdate iface key": false, "Alloc huge": false, "MakeSlice var": false,
	}
	for k, w := range wantSafe {
		c, ok := kinds[k]
		if !ok {
			t.Errorf("%s: not found in init", k)
			continue
		}
		if w && c[1] > 0 || !w && c[0] > 0 {
			t.Errorf("%s: safe %d unsafe %d, want all safe = %v", k, c[0], c[1], w)
		}
	}
}
