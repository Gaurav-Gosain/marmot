package codegen

import (
	"go/token"
	"go/types"
	"strings"

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

// elemAddr returns &base[i] for elements of Go type et after checking
// 0 <= i < n. idx is the SSA index (for constant folding and signedness).
func (f *funcGen) elemAddr(base, n string, nconst int64, et types.Type, idx ssa.Value) string {
	i := f.index64(idx)
	mt := f.tm.memType(et)
	if c, ok := idx.(*ssa.Const); ok && c.Value != nil && nconst >= 0 {
		if k := c.Int64(); k >= 0 && k < nconst {
			return f.gepDyn(base, mt, i) // statically in range
		}
	}
	if f.goOps() {
		uns := ""
		if !isSigned(idx.Type()) {
			uns = " unsigned"
		}
		return f.op("go.index %s[%s] len %s, %s%s : i64", base, i, n, mt, uns)
	}
	f.boundsCheck(i, n, isSigned(idx.Type()))
	return f.gepDyn(base, mt, i)
}

func (f *funcGen) indexAddr(v *ssa.IndexAddr) {
	x := f.value(v.X)
	switch u := v.X.Type().Underlying().(type) {
	case *types.Pointer: // *[N]T
		at := u.Elem().Underlying().(*types.Array)
		if needsNilCheck(v.X) {
			f.nilCheck(x)
		}
		n := f.constInt(at.Len(), mlir.I64)
		f.vals[v] = f.elemAddr(x, n, at.Len(), at.Elem(), v.Index)
	case *types.Slice:
		p := f.extractRaw(x, tSlice, 0)
		n := f.extractRaw(x, tSlice, 1)
		f.vals[v] = f.elemAddr(p, n, -1, u.Elem(), v.Index)
	default:
		f.fail(v.Pos(), "IndexAddr on %v", v.X.Type())
	}
}

func (f *funcGen) index(v *ssa.Index) {
	x := f.value(v.X)
	switch u := v.X.Type().Underlying().(type) {
	case *types.Basic: // string
		p := f.extractRaw(x, tString, 0)
		n := f.extractRaw(x, tString, 1)
		bp := f.elemAddr(p, n, -1, types.Typ[types.Uint8], v.Index)
		f.vals[v] = f.op("llvm.load %s : !llvm.ptr -> i8", bp)
	case *types.Array:
		at := f.tm.memType(v.X.Type())
		if f.tm.isBig(v.X.Type()) {
			n := f.constInt(u.Len(), mlir.I64)
			f.vals[v] = f.load(f.elemAddr(x, n, u.Len(), u.Elem(), v.Index), u.Elem())
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
		n := f.constInt(u.Len(), mlir.I64)
		slot := f.spill(x, v.X.Type())
		f.vals[v] = f.load(f.elemAddr(slot, n, u.Len(), u.Elem(), v.Index), u.Elem())
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
		if needsNilCheck(v.X) {
			f.nilCheck(x)
		}
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
	if f.goOps() {
		allUnsigned := true
		for _, x := range []ssa.Value{v.Low, v.High, v.Max} {
			if x != nil && isSigned(x.Type()) {
				allUnsigned = false
			}
		}
		flags := ""
		if _, isPtr := v.X.Type().Underlying().(*types.Pointer); isPtr {
			flags += " len_bound"
		}
		if allUnsigned {
			flags += " unsigned"
		}
		idx := lo + " : " + hi
		if v.Max != nil {
			idx += " : " + mx
		}
		rt := tSlice
		et := f.tm.memType(elem)
		if isStr {
			rt = tString
			et = mlir.I8
			flags = strings.Replace(flags, " len_bound", "", 1)
		}
		f.vals[v] = f.op("go.slice %s[%s] cap %s, %s%s : i64 -> %s", base, idx, capacity, et, flags, rt)
		return
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
	if f.stackMakeSlice(v, l, c) {
		return
	}
	if f.goOps() {
		f.vals[v] = f.op("go.make_slice %s, %s(%s, %s) : i64", mlir.Symbol(f.m.typeDescSym(st.Elem())), f.tm.memType(st.Elem()), l, c)
		return
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

// appendElems recognizes append(s, a, b, c): SSA builds a heap varargs
// array, stores the elements, slices it and appends the slice. When the
// array has no other use, the elements are returned (in order) and the
// array, its stores and the slice are skipped.
func (f *funcGen) findAppendElems() {
	f.appendElems = map[*ssa.Slice][]ssa.Value{}
	for _, b := range f.fn.Blocks {
		for _, instr := range b.Instrs {
			a, ok := instr.(*ssa.Alloc)
			if !ok || a.Comment != "varargs" {
				continue
			}
			at, ok := a.Type().(*types.Pointer).Elem().Underlying().(*types.Array)
			if !ok {
				continue
			}
			elems := make([]ssa.Value, at.Len())
			var skip []ssa.Instruction
			var sl *ssa.Slice
			good := true
			for _, r := range *a.Referrers() {
				switch x := r.(type) {
				case *ssa.IndexAddr:
					c, ok := x.Index.(*ssa.Const)
					refs := *x.Referrers()
					if !ok || len(refs) != 1 {
						good = false
						break
					}
					st, ok := refs[0].(*ssa.Store)
					i := c.Int64()
					if !ok || st.Addr != x || i < 0 || i >= at.Len() || elems[i] != nil {
						good = false
						break
					}
					elems[i] = st.Val
					skip = append(skip, x, st)
				case *ssa.Slice:
					if sl != nil || x.Low != nil || x.High != nil || x.Max != nil {
						good = false
						break
					}
					sl = x
				case *ssa.DebugRef:
				default:
					good = false
				}
			}
			if !good || sl == nil {
				continue
			}
			for _, e := range elems {
				if e == nil {
					good = false
				}
			}
			refs := *sl.Referrers()
			if !good || len(refs) != 1 {
				continue
			}
			call, ok := refs[0].(*ssa.Call)
			if !ok || len(call.Call.Args) != 2 || call.Call.Args[1] != ssa.Value(sl) {
				continue
			}
			if bi, ok := call.Call.Value.(*ssa.Builtin); !ok || bi.Name() != "append" {
				continue
			}
			if f.skipInstr == nil {
				f.skipInstr = map[ssa.Instruction]bool{}
			}
			f.skipInstr[a] = true
			f.skipInstr[sl] = true
			for _, i := range skip {
				f.skipInstr[i] = true
			}
			f.appendElems[sl] = elems
		}
	}
}

// append(s, t...) where t is a slice (or a string for []byte).
func (f *funcGen) append(sv, tv ssa.Value) string {
	if sl, ok := tv.(*ssa.Slice); ok && f.appendElems[sl] != nil {
		return f.appendValues(sv, f.appendElems[sl])
	}
	if ms, ok := tv.(*ssa.MakeSlice); ok && f.appendMake[ms] {
		return f.appendZeros(sv, ms.Len)
	}
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
	if f.reuseNow {
		f.freeOldArray(p, c, et) // the old array is unreachable now
	}
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

// appendValues appends individual elements: grow if needed, then store
// each element in place (no varargs array).
func (f *funcGen) appendValues(sv ssa.Value, elems []ssa.Value) string {
	s := f.value(sv)
	et := sv.Type().Underlying().(*types.Slice).Elem()
	vals := make([]string, len(elems))
	for i, e := range elems {
		vals[i] = f.value(e)
	}
	p := f.extractRaw(s, tSlice, 0)
	l := f.extractRaw(s, tSlice, 1)
	c := f.extractRaw(s, tSlice, 2)
	newLen := f.op("llvm.add %s, %s : i64", l, f.constInt(int64(len(elems)), mlir.I64))
	grow := f.op("llvm.icmp \"ugt\" %s, %s : i64", newLen, c)
	growB, done := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s weights([1, 100]), %s, %s(%s, %s : !llvm.ptr, i64)", grow, growB, done, p, c)
	saved := f.b
	f.b = &f.cold
	f.b.WriteString("  " + growB + ":\n")
	g := f.rtcall("growslice", f.typeDesc(et), p, l, c, newLen)
	if f.reuseNow {
		f.freeOldArray(p, c, et)
	}
	f.emit("llvm.br %s(%s, %s : !llvm.ptr, i64)", done, f.extractRaw(g, tSlice, 0), f.extractRaw(g, tSlice, 2))
	f.b = saved
	np, nc := f.newVal(), f.newVal()
	f.b.WriteString("  " + done + "(" + np + ": !llvm.ptr, " + nc + ": i64):\n")
	mt := f.tm.memType(et)
	for i, v := range vals {
		idx := l
		if i > 0 {
			idx = f.op("llvm.add %s, %s : i64", l, f.constInt(int64(i), mlir.I64))
		}
		f.store(v, f.gepDyn(np, mt, idx), et)
	}
	return f.build(tSlice, np, newLen, nc)
}

// findAppendMake finds append(s, make([]T, n)...) whose make has no other
// use: gc extends s by n zeroed elements without the temporary slice.
func (f *funcGen) findAppendMake() {
	if !optAppendMake {
		return
	}
	for _, b := range f.fn.Blocks {
		for _, instr := range b.Instrs {
			c, ok := instr.(*ssa.Call)
			if !ok || len(c.Call.Args) != 2 {
				continue
			}
			if bi, ok := c.Call.Value.(*ssa.Builtin); !ok || bi.Name() != "append" {
				continue
			}
			ms, ok := c.Call.Args[1].(*ssa.MakeSlice)
			if !ok || ms.Cap != ms.Len || ms.Block() != b {
				continue
			}
			only := true
			for _, r := range *ms.Referrers() {
				if _, dbg := r.(*ssa.DebugRef); !dbg && r != instr {
					only = false
				}
			}
			if !only {
				continue
			}
			if f.appendMake == nil {
				f.appendMake = map[*ssa.MakeSlice]bool{}
			}
			if f.skipInstr == nil {
				f.skipInstr = map[ssa.Instruction]bool{}
			}
			f.appendMake[ms] = true
			f.skipInstr[ms] = true
		}
	}
}

// appendZeros extends s by n zero elements (append(s, make([]T, n)...)).
func (f *funcGen) appendZeros(sv, nv ssa.Value) string {
	s := f.value(sv)
	et := sv.Type().Underlying().(*types.Slice).Elem()
	es := f.m.p.opts.Sizes.Sizeof(et)
	n := f.toInt64(f.value(nv), nv.Type())
	// make([]T, n) panics for a negative n before append runs.
	neg := f.op("llvm.icmp \"slt\" %s, %s : i64", n, f.constInt(0, mlir.I64))
	bad, ok := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s weights([1, 1000]), %s, %s", neg, bad, ok)
	f.b.WriteString("  " + bad + ":\n")
	f.rtcall("panicmakeslicelen")
	f.emit("llvm.unreachable")
	f.b.WriteString("  " + ok + ":\n")
	p := f.extractRaw(s, tSlice, 0)
	l := f.extractRaw(s, tSlice, 1)
	c := f.extractRaw(s, tSlice, 2)
	newLen := f.op("llvm.add %s, %s : i64", l, n)
	grow := f.op("llvm.icmp \"ugt\" %s, %s : i64", newLen, c)
	growB, done := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s weights([1, 100]), %s, %s(%s, %s : !llvm.ptr, i64)", grow, growB, done, p, c)
	f.b.WriteString("  " + growB + ":\n")
	g := f.rtcall("growslice", f.typeDesc(et), p, l, c, newLen)
	f.emit("llvm.br %s(%s, %s : !llvm.ptr, i64)", done, f.extractRaw(g, tSlice, 0), f.extractRaw(g, tSlice, 2))
	np, nc := f.newVal(), f.newVal()
	f.b.WriteString("  " + done + "(" + np + ": !llvm.ptr, " + nc + ": i64):\n")
	if es > 0 {
		dst := f.gepDyn(np, f.tm.memType(et), l)
		size := f.op("llvm.mul %s, %s : i64", n, f.constInt(es, mlir.I64))
		zero := f.constInt(0, mlir.I8)
		f.emit("\"llvm.intr.memset\"(%s, %s, %s) <{isVolatile = false}> : (!llvm.ptr, i8, i64) -> ()", dst, zero, size)
	}
	return f.build(tSlice, np, newLen, nc)
}
