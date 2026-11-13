package codegen

import (
	"fmt"
	"go/token"
	"go/types"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// ---- channels ----

func (f *funcGen) send(v *ssa.Send) {
	c := f.value(v.Chan)
	if f.goOps() && !f.tm.isBig(v.X.Type()) {
		f.emit("go.chan_send %s, %s : %s", c, f.value(v.X), f.tm.valType(v.X.Type()))
		return
	}
	p := f.spill(f.value(v.X), v.X.Type())
	f.rtcall("chansend1", c, p)
}

func (f *funcGen) recv(v *ssa.UnOp) string {
	c := f.value(v.X)
	et := v.X.Type().Underlying().(*types.Chan).Elem()
	if f.goOps() && !f.tm.isBig(et) {
		vt := f.tm.valType(et)
		if v.CommaOk {
			r, ok := f.newVal(), f.newVal()
			f.emit("%s, %s = go.chan_recv %s commaok : %s", r, ok, c, vt)
			return f.build(f.tm.valType(v.Type()), r, ok)
		}
		return f.op("go.chan_recv %s : %s", c, vt)
	}
	slot := f.zeroSlot(et)
	if v.CommaOk {
		ok := f.rtcall("chanrecv2", c, slot)
		val := f.load(slot, et)
		return f.build(f.tm.valType(v.Type()), val, ok)
	}
	f.rtcall("chanrecv1", c, slot)
	return f.load(slot, et)
}

// zeroSlot returns a zeroed stack slot for a value of type t.
func (f *funcGen) zeroSlot(t types.Type) string {
	mt := f.tm.memType(t)
	a := f.m.p.opts.Sizes.Alignof(t)
	if f.m.p.opts.Sizes.Sizeof(t) > maxStackVar {
		return f.newObject(t)
	}
	p := f.alloca(mt, a)
	if f.tm.isBig(t) {
		f.memset(p, f.m.p.opts.Sizes.Sizeof(t))
		return p
	}
	f.emit("llvm.store %s, %s {alignment = %d : i64} : %s, !llvm.ptr", f.zero(mt), p, a, mt)
	return p
}

var tScase = mlir.Struct(mlir.Ptr, mlir.Ptr, mlir.I8)

func (f *funcGen) selectStmt(v *ssa.Select) {
	n := len(v.States)
	blocking := f.op("llvm.mlir.constant(%v) : i1", v.Blocking)
	if n == 0 {
		r := f.rtcall("selectgo", f.null(), f.constInt(0, mlir.I64), blocking)
		f.vals[v] = f.build(f.tm.valType(v.Type()), f.extractRaw(r, f.selectRet(), 0), f.extractRaw(r, f.selectRet(), 1))
		return
	}
	cases := f.alloca(mlir.Array(int64(n), tScase), 8)
	var recvSlots []string
	var recvTypes []types.Type
	for i, st := range v.States {
		c := f.value(st.Chan)
		var elem string
		send := int64(0)
		if st.Dir == types.SendOnly {
			elem = f.spill(f.value(st.Send), st.Send.Type())
			send = 1
		} else {
			et := st.Chan.Type().Underlying().(*types.Chan).Elem()
			elem = f.zeroSlot(et)
			recvSlots = append(recvSlots, elem)
			recvTypes = append(recvTypes, et)
		}
		sc := f.build(tScase, c, elem, f.constInt(send, mlir.I8))
		p := f.gep(cases, mlir.Array(int64(n), tScase), 0, i)
		f.emit("llvm.store %s, %s {alignment = 8 : i64} : %s, !llvm.ptr", sc, p, tScase)
	}
	r := f.rtcall("selectgo", cases, f.constInt(int64(n), mlir.I64), blocking)
	rt := f.selectRet()
	vals := []string{f.extractRaw(r, rt, 0), f.extractRaw(r, rt, 1)}
	for i, s := range recvSlots {
		vals = append(vals, f.load(s, recvTypes[i]))
	}
	f.vals[v] = f.build(f.tm.valType(v.Type()), vals...)
}

func (f *funcGen) selectRet() string { return mlir.Struct(mlir.I64, mlir.I1) }

// ---- maps ----

func mapTypeOf(t types.Type) *types.Map { return t.Underlying().(*types.Map) }

func (f *funcGen) makeMap(v *ssa.MakeMap) {
	f.reportAlloc(v, "make-map", nil, "heap")
	hint := f.constInt(0, mlir.I64)
	if v.Reserve != nil {
		hint = f.toInt64(f.value(v.Reserve), v.Reserve.Type())
	}
	if f.goOps() {
		f.vals[v] = f.op("go.make_map %s, %s", mlir.Symbol(f.m.typeDescSym(v.Type())), hint)
		return
	}
	f.vals[v] = f.rtcall("makemap", f.typeDesc(v.Type()), hint)
}

func (f *funcGen) lookup(v *ssa.Lookup) {
	if u := f.fuseLookup[v]; u != nil {
		f.fusedLookup(v, u)
		return
	}
	x := f.value(v.X)
	if isString(v.X.Type()) {
		f.fail(v.Pos(), "string Lookup")
	}
	mt := mapTypeOf(v.X.Type())
	if f.goOps() && !f.tm.isBig(mt.Key()) && !f.tm.isBig(mt.Elem()) {
		kt, vt := f.tm.valType(mt.Key()), f.tm.valType(mt.Elem())
		k := f.value(v.Index)
		d := mlir.Symbol(f.m.typeDescSym(v.X.Type()))
		if v.CommaOk {
			r, ok := f.newVal(), f.newVal()
			f.emit("%s, %s = go.map_lookup %s[%s], %s commaok : %s -> %s", r, ok, x, k, d, kt, vt)
			f.vals[v] = f.build(f.tm.valType(v.Type()), r, ok)
			return
		}
		f.vals[v] = f.op("go.map_lookup %s[%s], %s : %s -> %s", x, k, d, kt, vt)
		return
	}
	if fk, kv := f.fastMapKey(mt.Key(), v.Index); fk != "" {
		desc := f.typeDesc(v.X.Type())
		if v.CommaOk {
			r := f.rtcall("mapaccess2_fast"+fk, append([]string{desc, x}, kv...)...)
			rt := mlir.Struct(mlir.Ptr, mlir.I1)
			val := f.load(f.extractRaw(r, rt, 0), mt.Elem())
			f.vals[v] = f.build(f.tm.valType(v.Type()), val, f.extractRaw(r, rt, 1))
			return
		}
		f.vals[v] = f.load(f.rtcall("mapaccess1_fast"+fk, append([]string{desc, x}, kv...)...), mt.Elem())
		return
	}
	key := f.spill(f.value(v.Index), mt.Key())
	desc := f.typeDesc(v.X.Type())
	if v.CommaOk {
		r := f.rtcall("mapaccess2", desc, x, key)
		rt := mlir.Struct(mlir.Ptr, mlir.I1)
		p := f.extractRaw(r, rt, 0)
		ok := f.extractRaw(r, rt, 1)
		val := f.load(p, mt.Elem())
		f.vals[v] = f.build(f.tm.valType(v.Type()), val, ok)
		return
	}
	p := f.rtcall("mapaccess1", desc, x, key)
	f.vals[v] = f.load(p, mt.Elem())
}

func (f *funcGen) mapUpdate(v *ssa.MapUpdate) {
	m := f.value(v.Map)
	mt := mapTypeOf(v.Map.Type())
	if slot, ok := f.fuseSlot[v]; ok {
		f.store(f.value(v.Value), slot, mt.Elem())
		return
	}
	if f.goOps() && !f.tm.isBig(mt.Key()) && !f.tm.isBig(mt.Elem()) {
		f.emit("go.map_update %s[%s] = %s, %s : %s, %s", m, f.value(v.Key), f.value(v.Value),
			mlir.Symbol(f.m.typeDescSym(v.Map.Type())), f.tm.valType(mt.Key()), f.tm.valType(mt.Elem()))
		return
	}
	if fk, kv := f.fastMapKey(mt.Key(), v.Key); fk != "" {
		val := f.value(v.Value)
		slot := f.rtcall("mapassign_fast"+fk, append([]string{f.typeDesc(v.Map.Type()), m}, kv...)...)
		f.store(val, slot, mt.Elem())
		return
	}
	key := f.spill(f.value(v.Key), mt.Key())
	val := f.value(v.Value)
	slot := f.rtcall("mapassign", f.typeDesc(v.Map.Type()), m, key)
	f.store(val, slot, mt.Elem())
}

func (f *funcGen) mapDelete(mv, kv ssa.Value) {
	mt := mapTypeOf(mv.Type())
	if f.goOps() && !f.tm.isBig(mt.Key()) {
		f.emit("go.map_delete %s[%s], %s : %s", f.value(mv), f.value(kv), mlir.Symbol(f.m.typeDescSym(mv.Type())), f.tm.valType(mt.Key()))
		return
	}
	if fk, k := f.fastMapKey(mt.Key(), kv); fk != "" {
		f.rtcall("mapdelete_fast"+fk, append([]string{f.typeDesc(mv.Type()), f.value(mv)}, k...)...)
		return
	}
	key := f.spill(f.value(kv), mt.Key())
	f.rtcall("mapdelete", f.typeDesc(mv.Type()), f.value(mv), key)
}

func (f *funcGen) clear(x ssa.Value) {
	switch u := x.Type().Underlying().(type) {
	case *types.Map:
		f.rtcall("mapclear", f.typeDesc(x.Type()), f.value(x))
	case *types.Slice:
		s := f.value(x)
		p := f.extractRaw(s, tSlice, 0)
		n := f.extractRaw(s, tSlice, 1)
		size := f.op("llvm.mul %s, %s : i64", n, f.constInt(f.m.p.opts.Sizes.Sizeof(u.Elem()), mlir.I64))
		zero := f.constInt(0, mlir.I8)
		f.emit("\"llvm.intr.memset\"(%s, %s, %s) <{isVolatile = false}> : (!llvm.ptr, i8, i64) -> ()", p, zero, size)
	default:
		f.fail(x.Pos(), "clear of %v", x.Type())
	}
}

// ---- range over strings and maps ----

// rangeStmt creates the iterator state for a Range. For strings it is a
// stack slot {string, next index}; for maps it is a runtime hiter.
func (f *funcGen) rangeStmt(v *ssa.Range) {
	x := f.value(v.X)
	switch u := v.X.Type().Underlying().(type) {
	case *types.Basic:
		st := mlir.Struct(tString, mlir.I64)
		slot := f.alloca(st, 8)
		f.emit("llvm.store %s, %s {alignment = 8 : i64} : %s, !llvm.ptr", f.build(st, x, f.constInt(0, mlir.I64)), slot, st)
		f.vals[v] = slot
	case *types.Map:
		ht := f.hiterType()
		slot := f.zeroSlot(ht)
		f.rtcall("mapiterinit", f.typeDesc(v.X.Type()), x, slot)
		f.vals[v] = slot
		_ = u
	default:
		f.fail(v.Pos(), "range over %v", v.X.Type())
	}
}

// hiterType returns the runtime's hiter type.
func (f *funcGen) hiterType() types.Type {
	obj := f.m.p.runtime.Pkg.Scope().Lookup("hiter")
	if obj == nil {
		f.fail(token.NoPos, "runtime.hiter is missing from lib/src/runtime (maps not implemented yet)")
	}
	return obj.Type()
}

func (f *funcGen) next(v *ssa.Next) {
	it := f.value(v.Iter)
	rt := f.tm.valType(v.Type())
	if v.IsString {
		st := mlir.Struct(tString, mlir.I64)
		s := f.load(f.gep(it, st, 0, 0), types.Typ[types.String])
		ip := f.gep(it, st, 0, 1)
		i := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> i64", ip)
		n := f.extractRaw(s, tString, 1)
		more := f.op("llvm.icmp \"slt\" %s, %s : i64", i, n)
		body, done := f.newBlock(), f.newBlock()
		zero := f.zeroOf(v.Type())
		f.emit("llvm.cond_br %s, %s, %s(%s : %s)", more, body, done, zero, rt)
		fmt.Fprintf(f.b, "  %s:\n", body)
		// ASCII fast path.
		p := f.extractRaw(s, tString, 0)
		bp := f.gepDyn(p, mlir.I8, i)
		c := f.op("llvm.load %s : !llvm.ptr -> i8", bp)
		ascii := f.op("llvm.icmp \"ult\" %s, %s : i8", c, f.constInt(0x80, mlir.I8))
		fast, slow, join := f.newBlock(), f.newBlock(), f.newBlock()
		f.emit("llvm.cond_br %s, %s, %s", ascii, fast, slow)
		fmt.Fprintf(f.b, "  %s:\n", fast)
		r1 := f.op("llvm.zext %s : i8 to i32", c)
		n1 := f.op("llvm.add %s, %s : i64", i, f.constInt(1, mlir.I64))
		f.emit("llvm.br %s(%s, %s : i32, i64)", join, r1, n1)
		fmt.Fprintf(f.b, "  %s:\n", slow)
		dr := f.rtcall("decoderune", s, i)
		drt := mlir.Struct(mlir.I32, mlir.I64)
		f.emit("llvm.br %s(%s, %s : i32, i64)", join, f.extractRaw(dr, drt, 0), f.extractRaw(dr, drt, 1))
		r, nx := f.newVal(), f.newVal()
		fmt.Fprintf(f.b, "  %s(%s: i32, %s: i64):\n", join, r, nx)
		f.emit("llvm.store %s, %s {alignment = 8 : i64} : i64, !llvm.ptr", nx, ip)
		tr := f.op("llvm.mlir.constant(true) : i1")
		rtt := v.Type().(*types.Tuple)
		ki, ri := i, r
		if isInvalid(rtt.At(1).Type()) {
			ki = f.constInt(0, mlir.I8)
		}
		if isInvalid(rtt.At(2).Type()) {
			ri = f.constInt(0, mlir.I8)
		}
		res := f.build(rt, tr, ki, ri)
		f.emit("llvm.br %s(%s : %s)", done, res, rt)
		out := f.newVal()
		fmt.Fprintf(f.b, "  %s(%s: %s):\n", done, out, rt)
		f.vals[v] = out
		return
	}
	// Map iteration: hiter.key == nil at the end.
	mt := mapTypeOf(v.Iter.(*ssa.Range).X.Type())
	ht := f.hiterType()
	hst, ok := ht.Underlying().(*types.Struct)
	if !ok {
		f.fail(v.Pos(), "runtime.hiter is not a struct")
	}
	keyIdx, elemIdx := -1, -1
	for i := 0; i < hst.NumFields(); i++ {
		switch hst.Field(i).Name() {
		case "key":
			keyIdx = i
		case "elem":
			elemIdx = i
		}
	}
	if keyIdx < 0 || elemIdx < 0 {
		f.fail(v.Pos(), "runtime.hiter needs key and elem fields")
	}
	hmt := f.tm.memType(ht)
	kp := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", f.gep(it, hmt, 0, keyIdx))
	more := f.op("llvm.icmp \"ne\" %s, %s : !llvm.ptr", kp, f.null())
	body, done := f.newBlock(), f.newBlock()
	zero := f.zeroOf(v.Type())
	f.emit("llvm.cond_br %s, %s, %s(%s : %s)", more, body, done, zero, rt)
	fmt.Fprintf(f.b, "  %s:\n", body)
	ep := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", f.gep(it, hmt, 0, elemIdx))
	rtt := v.Type().(*types.Tuple)
	var k, e string
	if isInvalid(rtt.At(1).Type()) {
		k = f.constInt(0, mlir.I8)
	} else {
		k = f.load(kp, mt.Key())
	}
	if isInvalid(rtt.At(2).Type()) {
		e = f.constInt(0, mlir.I8)
	} else {
		e = f.load(ep, mt.Elem())
	}
	f.rtcall("mapiternext", it)
	tr := f.op("llvm.mlir.constant(true) : i1")
	res := f.build(rt, tr, k, e)
	f.emit("llvm.br %s(%s : %s)", done, res, rt)
	out := f.newVal()
	fmt.Fprintf(f.b, "  %s(%s: %s):\n", done, out, rt)
	f.vals[v] = out
}

func isInvalid(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Kind() == types.Invalid
}

// fastMapKey picks the runtime's fast map entry points (key by value, as
// gc does): "32" and "64" for 4 and 8 byte integers and pointers, "str"
// for strings; "" otherwise (floats keep the generic path for NaN rules).
func (f *funcGen) fastMapKey(kt types.Type, k ssa.Value) (string, []string) {
	if f.m.p.runtimeFunc("mapaccess1_fast64") == nil {
		return "", nil
	}
	if f.plain128(kt) && f.m.p.runtimeFunc("mapaccess1_fast128") != nil {
		// Two words, passed by value (k0, k1 uint64).
		p := f.spill(f.value(k), kt)
		k0 := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> i64", p)
		p1 := f.op("llvm.getelementptr %s[8] : (!llvm.ptr) -> !llvm.ptr, i8", p)
		k1 := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> i64", p1)
		return "128", []string{k0, k1}
	}
	kv := f.value(k)
	switch u := kt.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.Int32, types.Uint32:
			return "32", []string{kv}
		case types.Int, types.Uint, types.Int64, types.Uint64, types.Uintptr:
			return "64", []string{kv}
		case types.String:
			return "str", []string{kv}
		case types.UnsafePointer:
			return "64", []string{f.op("llvm.ptrtoint %s : !llvm.ptr to i64", kv)}
		}
	case *types.Pointer, *types.Chan:
		return "64", []string{f.op("llvm.ptrtoint %s : !llvm.ptr to i64", kv)}
	}
	return "", nil
}

// plain128 reports whether t is a 16 byte key made only of integers with no
// padding, so key equality is equality of its two words.
func (f *funcGen) plain128(t types.Type) bool {
	sz := f.m.p.opts.Sizes
	if sz.Sizeof(t) != 16 {
		return false
	}
	var plain func(t types.Type) bool
	plain = func(t types.Type) bool {
		switch u := t.Underlying().(type) {
		case *types.Basic:
			return u.Info()&types.IsInteger != 0
		case *types.Array:
			return plain(u.Elem())
		case *types.Struct:
			var n int64
			for field := range u.Fields() {
				ft := field.Type()
				if !plain(ft) || field.Name() == "_" {
					return false
				}
				n += sz.Sizeof(ft)
			}
			return n == sz.Sizeof(t)
		}
		return false
	}
	return plain(t)
}
