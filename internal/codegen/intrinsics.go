package codegen

import (
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Atomic intrinsics: body-less functions in these packages are expanded
// inline (see lib/src/internal/runtime/atomic/atomic_marmot.go).
var atomicPkgs = map[string]bool{
	"internal/runtime/atomic": true,
	"sync/atomic":             true,
}

type atomicOp struct {
	kind string // load, store, rmw, cas
	rmw  string // add, xchg, and, or
	typ  string // i8, i32, i64, ptr
	ret  string // "new", "old", "none" for rmw
}

var atomicOps = map[string]atomicOp{
	"Load8": {"load", "", "i8", ""}, "Load": {"load", "", "i32", ""}, "Load64": {"load", "", "i64", ""},
	"Loadp":  {"load", "", "ptr", ""},
	"Store8": {"store", "", "i8", ""}, "Store": {"store", "", "i32", ""}, "Store64": {"store", "", "i64", ""},
	"StorepNoWB": {"store", "", "ptr", ""},
	"Xadd":       {"rmw", "add", "i32", "new"}, "Xadd64": {"rmw", "add", "i64", "new"},
	"Xchg8": {"rmw", "xchg", "i8", "old"}, "Xchg": {"rmw", "xchg", "i32", "old"}, "Xchg64": {"rmw", "xchg", "i64", "old"},
	"Cas": {"cas", "", "i32", ""}, "Cas64": {"cas", "", "i64", ""}, "Casp1": {"cas", "", "ptr", ""},
	"And8": {"rmw", "_and", "i8", "none"}, "Or8": {"rmw", "_or", "i8", "none"},
	"And": {"rmw", "_and", "i32", "none"}, "Or": {"rmw", "_or", "i32", "none"},
	"And32": {"rmw", "_and", "i32", "old"}, "Or32": {"rmw", "_or", "i32", "old"},
	"And64": {"rmw", "_and", "i64", "old"}, "Or64": {"rmw", "_or", "i64", "old"},
}

// intrinsic expands a call to fn inline if it is an intrinsic. ok is false
// when fn is an ordinary function.
func (f *funcGen) intrinsic(fn *ssa.Function, args []string) (res string, ok bool) {
	if fn.Pkg != nil && fn.Pkg.Pkg.Path() == "math/bits" && fn.Signature.Recv() == nil {
		if r, ok := f.bitsIntrinsic(fn, args); ok {
			return r, true
		}
	}
	if !f.m.p.bodyless(fn) || fn.Pkg == nil {
		return "", false
	}
	if fn.Pkg.Pkg.Path() == "runtime" && fn.Name() == "getcallerfp" {
		fp := f.op("llvm.call_intrinsic \"llvm.frameaddress.p0\"(%s) : (i32) -> !llvm.ptr", f.constInt(0, "i32"))
		return f.op("llvm.ptrtoint %s : !llvm.ptr to i64", fp), true
	}
	if fn.Pkg.Pkg.Path() == "internal/abi" && (fn.Name() == "FuncPCABI0" || fn.Name() == "FuncPCABIInternal") {
		return f.funcPC(), true
	}
	if !atomicPkgs[fn.Pkg.Pkg.Path()] {
		return "", false
	}
	op, found := atomicOps[fn.Name()]
	if !found {
		return "", false
	}
	t := op.typ
	if t == "ptr" {
		t = "!llvm.ptr"
	}
	align := map[string]int{"i8": 1, "i32": 4, "i64": 8, "ptr": 8}[op.typ]
	switch op.kind {
	case "load":
		return f.op("llvm.load %s atomic seq_cst {alignment = %d : i64} : !llvm.ptr -> %s", args[0], align, t), true
	case "store":
		f.emit("llvm.store %s, %s atomic seq_cst {alignment = %d : i64} : %s, !llvm.ptr", args[1], args[0], align, t)
		return "", true
	case "rmw":
		name := op.rmw
		old := f.op("llvm.atomicrmw %s %s, %s seq_cst : !llvm.ptr, %s", name, args[0], args[1], t)
		switch op.ret {
		case "old":
			return old, true
		case "new":
			return f.op("llvm.add %s, %s : %s", old, args[1], t), true
		}
		return "", true
	case "cas":
		pair := f.op("llvm.cmpxchg %s, %s, %s seq_cst seq_cst : !llvm.ptr, %s", args[0], args[1], args[2], t)
		return f.op("llvm.extractvalue %s[1] : !llvm.struct<(%s, i1)>", pair, map[string]string{"i8": "i8", "i32": "i32", "i64": "i64", "ptr": "ptr"}[op.typ]), true
	}
	return "", false
}

// funcPC implements internal/abi.FuncPCABI0 and FuncPCABIInternal: the code
// address of a function value. A body-less libc_X_trampoline resolves to
// the C symbol named by its //go:cgo_import_dynamic directive.
func (f *funcGen) funcPC() string {
	arg := f.curArgs[0]
	if mi, ok := arg.(*ssa.MakeInterface); ok {
		if fn, ok := mi.X.(*ssa.Function); ok && len(fn.FreeVars) == 0 {
			sym := ""
			if f.m.p.bodyless(fn) && fn.Pkg != nil && strings.HasSuffix(fn.Name(), "_trampoline") {
				local := strings.TrimSuffix(fn.Name(), "_trampoline")
				if c, ok := f.m.p.dynamic[fn.Pkg.Pkg.Path()+"."+local]; ok {
					sym = c
					f.m.declareRaw(sym, nil, "", "")
				}
			}
			if sym == "" {
				sym = f.m.funcRef(fn)
			}
			return f.op("llvm.ptrtoint %s : !llvm.ptr to i64", f.addressOf(sym))
		}
	}
	// Dynamic func value: load the code pointer from the closure.
	x := f.value(arg)
	fv := f.extractRaw(x, tIface, 1)
	code := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", fv)
	return f.op("llvm.ptrtoint %s : !llvm.ptr to i64", code)
}

// bitsIntrinsic lowers math/bits functions to LLVM intrinsics, as gc does.
func (f *funcGen) bitsIntrinsic(fn *ssa.Function, args []string) (string, bool) {
	name := fn.Name()
	width := func(suffix string) (string, int) {
		switch suffix {
		case "8":
			return "i8", 8
		case "16":
			return "i16", 16
		case "32":
			return "i32", 32
		}
		return "i64", 64 // "64" and "" (uint)
	}
	call := func(intr, t string, a ...string) string {
		ts := make([]string, len(a))
		for i := range a {
			ts[i] = t
		}
		return f.op("llvm.call_intrinsic %q(%s) : (%s) -> %s", intr, strings.Join(a, ", "), strings.Join(ts, ", "), t)
	}
	toInt := func(v, t string) string {
		if t == "i64" {
			return v
		}
		return f.op("llvm.zext %s : %s to i64", v, t)
	}
	for _, p := range []struct {
		prefix, intr string
	}{{"TrailingZeros", "cttz"}, {"LeadingZeros", "ctlz"}, {"Len", "ctlz"}, {"OnesCount", "ctpop"}} {
		if !strings.HasPrefix(name, p.prefix) {
			continue
		}
		t, w := width(strings.TrimPrefix(name, p.prefix))
		if strings.TrimPrefix(name, p.prefix) != "" && t == "i64" && strings.TrimPrefix(name, p.prefix) != "64" {
			return "", false
		}
		var r string
		if p.intr == "ctpop" {
			r = f.op("llvm.call_intrinsic \"llvm.ctpop.%s\"(%s) : (%s) -> %s", t, args[0], t, t)
		} else {
			fls := f.op("llvm.mlir.constant(false) : i1")
			r = f.op("llvm.call_intrinsic \"llvm.%s.%s\"(%s, %s) : (%s, i1) -> %s", p.intr, t, args[0], fls, t, t)
		}
		r = toInt(r, t)
		if p.prefix == "Len" {
			r = f.op("llvm.sub %s, %s : i64", f.constInt(int64(w), mlir.I64), r)
		}
		return r, true
	}
	switch {
	case strings.HasPrefix(name, "RotateLeft"):
		t, _ := width(strings.TrimPrefix(name, "RotateLeft"))
		k := args[1]
		if t != "i64" {
			k = f.op("llvm.trunc %s : i64 to %s", k, t)
		}
		return f.op("llvm.call_intrinsic \"llvm.fshl.%s\"(%s, %s, %s) : (%s, %s, %s) -> %s", t, args[0], args[0], k, t, t, t, t), true
	case strings.HasPrefix(name, "ReverseBytes"):
		t, w := width(strings.TrimPrefix(name, "ReverseBytes"))
		if w == 8 {
			return "", false
		}
		return call("llvm.bswap."+t, t, args[0]), true
	case strings.HasPrefix(name, "Reverse"):
		t, _ := width(strings.TrimPrefix(name, "Reverse"))
		return call("llvm.bitreverse."+t, t, args[0]), true
	case name == "Mul64" || name == "Mul":
		a := f.op("llvm.zext %s : i64 to i128", args[0])
		b := f.op("llvm.zext %s : i64 to i128", args[1])
		p := f.op("llvm.mul %s, %s : i128", a, b)
		lo := f.op("llvm.trunc %s : i128 to i64", p)
		hi := f.op("llvm.trunc %s : i128 to i64", f.op("llvm.lshr %s, %s : i128", p, f.op("llvm.mlir.constant(64 : i128) : i128")))
		return f.build(mlir.Struct(mlir.I64, mlir.I64), hi, lo), true
	case name == "Add64" || name == "Add":
		a := f.op("llvm.zext %s : i64 to i128", args[0])
		b := f.op("llvm.zext %s : i64 to i128", args[1])
		c := f.op("llvm.zext %s : i64 to i128", args[2])
		s := f.op("llvm.add %s, %s : i128", f.op("llvm.add %s, %s : i128", a, b), c)
		sum := f.op("llvm.trunc %s : i128 to i64", s)
		carry := f.op("llvm.trunc %s : i128 to i64", f.op("llvm.lshr %s, %s : i128", s, f.op("llvm.mlir.constant(64 : i128) : i128")))
		return f.build(mlir.Struct(mlir.I64, mlir.I64), sum, carry), true
	case name == "Sub64" || name == "Sub":
		d1 := f.op("llvm.sub %s, %s : i64", args[0], args[1])
		d := f.op("llvm.sub %s, %s : i64", d1, args[2])
		// borrowOut = ((^x & y) | (^(x ^ y) & diff)) >> 63, as in math/bits.
		m1 := f.constInt(-1, mlir.I64)
		nx := f.op("llvm.xor %s, %s : i64", args[0], m1)
		t1 := f.op("llvm.and %s, %s : i64", nx, args[1])
		xy := f.op("llvm.xor %s, %s : i64", args[0], args[1])
		nxy := f.op("llvm.xor %s, %s : i64", xy, m1)
		t2 := f.op("llvm.and %s, %s : i64", nxy, d)
		bo := f.op("llvm.lshr %s, %s : i64", f.op("llvm.or %s, %s : i64", t1, t2), f.constInt(63, mlir.I64))
		return f.build(mlir.Struct(mlir.I64, mlir.I64), d, bo), true
	}
	return "", false
}
