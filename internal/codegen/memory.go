package codegen

import (
	"go/token"
	"go/types"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Bounds failure codes of runtime.panicBounds (lib/src/runtime/error.go).
const (
	boundsIndex      = 0
	boundsSliceAlen  = 1
	boundsSliceAcap  = 2
	boundsSliceB     = 3
	boundsSlice3Alen = 4
	boundsSlice3Acap = 5
	boundsSlice3B    = 6
	boundsSlice3C    = 7
	boundsConvert    = 8
)

// boundsCheck panics unless 0 <= i < n. i and n are i64; the compare is
// unsigned so negative values fail.
func (f *funcGen) boundsCheck(i, n string, signed bool) {
	f.checkLE(boundsIndex, i, n, signed, true)
}

// checkLE panics with panicBounds(code, x, signed, y) unless x <= y
// (x < y when strict), comparing unsigned.
func (f *funcGen) checkLE(code int, x, y string, signed, strict bool) {
	pred := "ugt"
	if strict {
		pred = "uge"
	}
	bad := f.op("llvm.icmp %q %s, %s : i64", pred, x, y)
	f.branchCold(bad, func() {
		c := f.constInt(int64(code), mlir.I64)
		s := f.op("llvm.mlir.constant(%v) : i1", signed)
		f.rtcall("panicBounds", c, x, s, y)
		f.emit("llvm.unreachable")
	})
}

// index64 converts an SSA index value to i64. Unsigned indices wider than
// int stay unsigned through the bounds check because the compare is unsigned.
func (f *funcGen) index64(v ssa.Value) string {
	if c, ok := v.(*ssa.Const); ok && c.Value != nil {
		_ = c
	}
	return f.toInt64(f.value(v), v.Type())
}

func (f *funcGen) indexAddr(v *ssa.IndexAddr) {
	x := f.value(v.X)
	i := f.index64(v.Index)
	switch u := v.X.Type().Underlying().(type) {
	case *types.Pointer: // *[N]T
		at := u.Elem().Underlying().(*types.Array)
		f.nilCheck(x)
		n := f.constInt(at.Len(), mlir.I64)
		f.boundsCheck(i, n, isSigned(v.Index.Type()))
		f.vals[v] = f.gepDyn(x, f.tm.memType(at.Elem()), i)
	case *types.Slice:
		p := f.extractRaw(x, tSlice, 0)
		n := f.extractRaw(x, tSlice, 1)
		f.boundsCheck(i, n, isSigned(v.Index.Type()))
		f.vals[v] = f.gepDyn(p, f.tm.memType(u.Elem()), i)
	default:
		f.fail(v.Pos(), "IndexAddr on %v", v.X.Type())
	}
}

func (f *funcGen) index(v *ssa.Index) {
	x := f.value(v.X)
	switch u := v.X.Type().Underlying().(type) {
	case *types.Basic: // string
		i := f.index64(v.Index)
		p := f.extractRaw(x, tString, 0)
		n := f.extractRaw(x, tString, 1)
		f.boundsCheck(i, n, isSigned(v.Index.Type()))
		bp := f.gepDyn(p, mlir.I8, i)
		f.vals[v] = f.op("llvm.load %s : !llvm.ptr -> i8", bp)
	case *types.Array:
		at := f.tm.memType(v.X.Type())
		if f.tm.isBig(v.X.Type()) {
			i := f.index64(v.Index)
			n := f.constInt(u.Len(), mlir.I64)
			f.boundsCheck(i, n, isSigned(v.Index.Type()))
			f.vals[v] = f.load(f.gepDyn(x, f.tm.memType(u.Elem()), i), u.Elem())
			return
		}
		if c, ok := v.Index.(*ssa.Const); ok && c.Value != nil {
			i := c.Int64()
			if i < 0 || i >= u.Len() {
				f.fail(v.Pos(), "constant index %d out of range", i)
			}
			f.vals[v] = f.extract(x, at, int(i), u.Elem())
			return
		}
		i := f.index64(v.Index)
		n := f.constInt(u.Len(), mlir.I64)
		f.boundsCheck(i, n, isSigned(v.Index.Type()))
		slot := f.spill(x, v.X.Type())
		f.vals[v] = f.load(f.gepDyn(slot, f.tm.memType(u.Elem()), i), u.Elem())
	default:
		f.fail(v.Pos(), "Index on %v", v.X.Type())
	}
}

// slice implements s[lo:hi:max] for strings, slices and array pointers.
func (f *funcGen) slice(v *ssa.Slice) {
	x := f.value(v.X)
	var base, length, capacity string
	var elem types.Type
	isStr := false
	switch u := v.X.Type().Underlying().(type) {
	case *types.Basic:
		isStr = true
		base = f.extractRaw(x, tString, 0)
		length = f.extractRaw(x, tString, 1)
		capacity = length
		elem = types.Typ[types.Uint8]
	case *types.Slice:
		base = f.extractRaw(x, tSlice, 0)
		length = f.extractRaw(x, tSlice, 1)
		capacity = f.extractRaw(x, tSlice, 2)
		elem = u.Elem()
	case *types.Pointer:
		at := u.Elem().Underlying().(*types.Array)
		f.nilCheck(x)
		base = x
		length = f.constInt(at.Len(), mlir.I64)
		capacity = length
		elem = at.Elem()
	default:
		f.fail(v.Pos(), "slice of %v", v.X.Type())
	}
	lo := f.constInt(0, mlir.I64)
	if v.Low != nil {
		lo = f.index64(v.Low)
	}
	hi := length
	if v.High != nil {
		hi = f.index64(v.High)
	}
	mx := capacity
	if v.Max != nil {
		mx = f.index64(v.Max)
	}
	// 0 <= lo <= hi <= max <= cap, unsigned compares, checked in the order
	// gc checks them so the panic message matches.
	sgn := func(x ssa.Value) bool { return x != nil && isSigned(x.Type()) }
	_, isSlice := v.X.Type().Underlying().(*types.Slice)
	if v.Max != nil {
		code := boundsSlice3Alen
		if isSlice {
			code = boundsSlice3Acap
		}
		f.checkLE(code, mx, capacity, sgn(v.Max), false)
		f.checkLE(boundsSlice3B, hi, mx, sgn(v.High), false)
		f.checkLE(boundsSlice3C, lo, hi, sgn(v.Low), false)
	} else {
		if v.High != nil {
			code := boundsSliceAlen
			if isSlice {
				code = boundsSliceAcap
			}
			f.checkLE(code, hi, capacity, sgn(v.High), false)
		}
		if v.Low != nil {
			f.checkLE(boundsSliceB, lo, hi, sgn(v.Low), false)
		}
	}
	newLen := f.op("llvm.sub %s, %s : i64", hi, lo)
	newCap := f.op("llvm.sub %s, %s : i64", mx, lo)
	// Do not advance the pointer past the end when the result is empty:
	// keep base so no pointer to the next object is created.
	adv := f.gepDyn(base, f.tm.memType(elem), lo)
	z := f.constInt(0, mlir.I64)
	var empty string
	if isStr {
		empty = f.op("llvm.icmp \"eq\" %s, %s : i64", newLen, z)
	} else {
		empty = f.op("llvm.icmp \"eq\" %s, %s : i64", newCap, z)
	}
	ptr := f.op("llvm.select %s, %s, %s : i1, !llvm.ptr", empty, base, adv)
	if isStr {
		f.vals[v] = f.makeString(ptr, newLen)
		return
	}
	f.vals[v] = f.build(tSlice, ptr, newLen, newCap)
}

func (f *funcGen) makeSlice(v *ssa.MakeSlice) {
	st := v.Type().Underlying().(*types.Slice)
	l := f.toInt64(f.value(v.Len), v.Len.Type())
	c := f.toInt64(f.value(v.Cap), v.Cap.Type())
	if isSigned(v.Len.Type()) || isSigned(v.Cap.Type()) || true {
		// makeslice checks len and cap (negative, len > cap, too large).
	}
	p := f.rtcall("makeslice", f.typeDesc(st.Elem()), l, c)
	f.vals[v] = f.build(tSlice, p, l, c)
}

func (f *funcGen) sliceToArrayPointer(v *ssa.SliceToArrayPointer) {
	x := f.value(v.X)
	at := v.Type().Underlying().(*types.Pointer).Elem().Underlying().(*types.Array)
	n := f.extractRaw(x, tSlice, 1)
	want := f.constInt(at.Len(), mlir.I64)
	bad := f.op("llvm.icmp \"ult\" %s, %s : i64", n, want)
	f.branchCold(bad, func() {
		c := f.constInt(boundsConvert, mlir.I64)
		s := f.op("llvm.mlir.constant(false) : i1")
		f.rtcall("panicBounds", c, n, s, want)
		f.emit("llvm.unreachable")
	})
	f.vals[v] = f.extractRaw(x, tSlice, 0)
}

// append(s, t...) where t is a slice (or a string for []byte).
func (f *funcGen) append(sv, tv ssa.Value) string {
	s := f.value(sv)
	t := f.value(tv)
	st := sv.Type().Underlying().(*types.Slice)
	et := st.Elem()
	es := f.m.p.opts.Sizes.Sizeof(et)
	tt := f.tm.valType(tv.Type())
	tp, tl := f.extractRaw(t, tt, 0), f.extractRaw(t, tt, 1)
	p := f.extractRaw(s, tSlice, 0)
	l := f.extractRaw(s, tSlice, 1)
	c := f.extractRaw(s, tSlice, 2)
	newLen := f.op("llvm.add %s, %s : i64", l, tl)
	grow := f.op("llvm.icmp \"ugt\" %s, %s : i64", newLen, c)
	growB, done := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s weights([1, 100]), %s, %s(%s, %s : !llvm.ptr, i64)", grow, growB, done, p, c)
	saved := f.b
	f.b = &f.cold
	f.b.WriteString("  " + growB + ":\n")
	g := f.rtcall("growslice", f.typeDesc(et), p, l, c, newLen)
	gp := f.extractRaw(g, tSlice, 0)
	gc := f.extractRaw(g, tSlice, 2)
	f.emit("llvm.br %s(%s, %s : !llvm.ptr, i64)", done, gp, gc)
	f.b = saved
	np, nc := f.newVal(), f.newVal()
	f.b.WriteString("  " + done + "(" + np + ": !llvm.ptr, " + nc + ": i64):\n")
	dst := f.gepDyn(np, f.tm.memType(et), l)
	size := f.op("llvm.mul %s, %s : i64", tl, f.constInt(es, mlir.I64))
	f.emit("\"llvm.intr.memmove\"(%s, %s, %s) <{isVolatile = false}> : (!llvm.ptr, !llvm.ptr, i64) -> ()", dst, tp, size)
	return f.build(tSlice, np, newLen, nc)
}

var _ = token.NoPos
