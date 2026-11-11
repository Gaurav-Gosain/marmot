package codegen

import (
	"fmt"
	"go/constant"
	"go/types"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// typeWord returns the first word for an interface holding a T: the type
// descriptor for empty interfaces, the itab otherwise.
func (f *funcGen) typeWord(t, iface types.Type) string {
	if isEmptyInterface(iface) {
		return f.typeDesc(t)
	}
	return f.addressOf(f.m.itabSym(t, iface))
}

func (f *funcGen) makeInterface(v *ssa.MakeInterface) {
	t := v.X.Type()
	tw := f.typeWord(t, v.Type())
	data := f.boxValue(v.X)
	f.vals[v] = f.build(tIface, tw, data)
}

// boxValue returns the interface data word for value x.
func (f *funcGen) boxValue(x ssa.Value) string {
	t := x.Type()
	if isDirectIface(t) {
		return f.toDataWord(f.value(x), t)
	}
	if f.m.p.opts.Sizes.Sizeof(t) == 0 {
		return f.addressOf(f.m.zeroBase())
	}
	if c, ok := x.(*ssa.Const); ok {
		return f.addressOf(f.m.constBox(c))
	}
	p := f.newObject(t)
	f.store(f.value(x), p, t)
	return p
}

// toDataWord converts a pointer-shaped value to its ptr representation.
func (f *funcGen) toDataWord(v string, t types.Type) string {
	if f.tm.valType(t) == mlir.Ptr {
		return v
	}
	// Single element struct or array holding a pointer.
	slot := f.spill(v, t)
	return f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", slot)
}

// fromDataWord converts an interface data word to a value of type t.
func (f *funcGen) fromDataWord(data string, t types.Type) string {
	if isDirectIface(t) {
		if f.tm.valType(t) == mlir.Ptr {
			return data
		}
		slot := f.alloca(mlir.Ptr, 8)
		f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", data, slot)
		return f.load(slot, t)
	}
	if f.m.p.opts.Sizes.Sizeof(t) == 0 {
		return f.zero(f.tm.valType(t))
	}
	return f.load(data, t)
}

// zeroBase is a symbol for the address of zero sized values.
func (m *moduleGen) zeroBase() string {
	sym := "runtime.zerobase.marmot"
	if !m.defined[sym] {
		m.constGlobal(sym, "linkonce_odr", true, cInt(0, mlir.I64))
	}
	return sym
}

// constBox makes a private constant holding the value of c, so converting
// a constant to an interface does not allocate.
func (m *moduleGen) constBox(c *ssa.Const) string {
	exact := "zero"
	if c.Value != nil {
		exact = c.Value.ExactString()
	}
	key := fmt.Sprintf("%s|%s", m.p.canonical(c.Type()), exact)
	if sym, ok := m.strs["box:"+key]; ok {
		return sym
	}
	sym := fmt.Sprintf(".box.%d", m.nstr)
	m.nstr++
	m.strs["box:"+key] = sym
	t := c.Type()
	mt := m.tm.memType(t)
	var val cv
	switch {
	case c.Value == nil:
		val = cZero(mt)
	case isString(t):
		val = m.cString(constant.StringVal(c.Value))
	case isBool(t):
		b := int64(0)
		if constant.BoolVal(c.Value) {
			b = 1
		}
		val = cInt(b, mlir.I8)
	case isInteger(t):
		var bits uint64
		if i, ok := constant.Int64Val(c.Value); ok {
			bits = uint64(i)
		} else {
			bits, _ = constant.Uint64Val(c.Value)
		}
		val = cInt(int64(bits), mt)
	case isFloat(t):
		fv, _ := constant.Float64Val(constant.ToFloat(c.Value))
		val = cFloat(fv, mt)
	case isComplex(t):
		et := complexElem(mt)
		re, _ := constant.Float64Val(constant.ToFloat(constant.Real(c.Value)))
		im, _ := constant.Float64Val(constant.ToFloat(constant.Imag(c.Value)))
		val = cAgg(mt, cFloat(re, et), cFloat(im, et))
	default:
		val = cZero(mt)
	}
	m.constGlobal(sym, "private", true, val)
	return sym
}

func cFloat(v float64, typ string) cv { return cv{typ: typ, kind: cvFloat, f: v} }

// dynType returns the dynamic type descriptor of interface value x.
func (f *funcGen) dynType(x string, xt types.Type) string {
	w := f.extractRaw(x, tIface, 0)
	if isEmptyInterface(xt) {
		return w
	}
	// itab.typ, or nil for a nil interface.
	n := f.null()
	isNil := f.op("llvm.icmp \"eq\" %s, %s : !llvm.ptr", w, n)
	nb, done := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s, %s(%s : !llvm.ptr), %s", isNil, done, n, nb)
	fmt.Fprintf(f.b, "  %s:\n", nb)
	tp := f.gepByte(w, 8)
	t := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", tp)
	f.emit("llvm.br %s(%s : !llvm.ptr)", done, t)
	r := f.newVal()
	fmt.Fprintf(f.b, "  %s(%s: !llvm.ptr):\n", done, r)
	return r
}

func (f *funcGen) typeAssert(v *ssa.TypeAssert) {
	x := f.value(v.X)
	xt := v.X.Type()
	at := v.AssertedType
	data := f.extractRaw(x, tIface, 1)
	if isInterface(at) {
		typ := f.dynType(x, xt)
		var tw string
		var ok string
		if isEmptyInterface(at) {
			tw = typ
			n := f.null()
			ok = f.op("llvm.icmp \"ne\" %s, %s : !llvm.ptr", typ, n)
			if !v.CommaOk {
				f.branchCold(f.not(ok), func() {
					f.rtcall("assertE2I", f.typeDesc(at), typ) // panics for nil
					f.emit("llvm.unreachable")
				})
			}
		} else if v.CommaOk {
			tw = f.rtcall("assertE2I2", f.typeDesc(at), typ)
			n := f.null()
			ok = f.op("llvm.icmp \"ne\" %s, %s : !llvm.ptr", tw, n)
		} else {
			tw = f.rtcall("assertE2I", f.typeDesc(at), typ)
		}
		if !v.CommaOk {
			f.vals[v] = f.build(tIface, tw, data)
			return
		}
		z := f.null()
		d := f.op("llvm.select %s, %s, %s : i1, !llvm.ptr", ok, data, z)
		tw = f.op("llvm.select %s, %s, %s : i1, !llvm.ptr", ok, tw, z)
		res := f.build(tIface, tw, d)
		f.vals[v] = f.build(f.tm.valType(v.Type()), res, ok)
		return
	}
	// Concrete type: compare the type word.
	var ok string
	want := f.typeDesc(at)
	// Itabs built at run time differ from compiler emitted ones, so compare
	// the dynamic type, not the itab.
	dyn := f.dynType(x, xt)
	ok = f.op("llvm.icmp \"eq\" %s, %s : !llvm.ptr", dyn, want)
	if !v.CommaOk {
		f.branchCold(f.not(ok), func() {
			f.rtcall("panicTypeAssert", dyn, want, f.typeDesc(xt))
			f.emit("llvm.unreachable")
		})
		f.vals[v] = f.fromDataWord(data, at)
		return
	}
	vt := f.tm.valType(at)
	yes, done := f.newBlock(), f.newBlock()
	z := f.zero(vt)
	f.emit("llvm.cond_br %s, %s, %s(%s : %s)", ok, yes, done, z, vt)
	fmt.Fprintf(f.b, "  %s:\n", yes)
	val := f.fromDataWord(data, at)
	f.emit("llvm.br %s(%s : %s)", done, val, vt)
	r := f.newVal()
	fmt.Fprintf(f.b, "  %s(%s: %s):\n", done, r, vt)
	f.vals[v] = f.build(f.tm.valType(v.Type()), r, ok)
}

// changeInterface converts between interface types (always succeeds).
func (f *funcGen) changeInterface(v *ssa.ChangeInterface) {
	x := f.value(v.X)
	to := v.Type()
	if isEmptyInterface(to) {
		typ := f.dynType(x, v.X.Type())
		f.vals[v] = f.build(tIface, typ, f.extractRaw(x, tIface, 1))
		return
	}
	// Non-empty target: look up the itab, nil stays nil.
	typ := f.dynType(x, v.X.Type())
	n := f.null()
	isNil := f.op("llvm.icmp \"eq\" %s, %s : !llvm.ptr", typ, n)
	nb, done := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s, %s(%s : !llvm.ptr), %s", isNil, done, n, nb)
	fmt.Fprintf(f.b, "  %s:\n", nb)
	tab := f.rtcall("assertE2I", f.typeDesc(to), typ)
	f.emit("llvm.br %s(%s : !llvm.ptr)", done, tab)
	r := f.newVal()
	fmt.Fprintf(f.b, "  %s(%s: !llvm.ptr):\n", done, r)
	f.vals[v] = f.build(tIface, r, f.extractRaw(x, tIface, 1))
}
