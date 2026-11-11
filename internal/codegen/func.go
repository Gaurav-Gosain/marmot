package codegen

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// funcGen translates one SSA function.
type funcGen struct {
	m    *moduleGen
	fn   *ssa.Function
	tm   *typeMapper
	b    *strings.Builder // current output
	body strings.Builder
	pro  strings.Builder // allocas, emitted at the top of the entry block
	cold strings.Builder // panic paths, emitted after all blocks

	vals    map[ssa.Value]string
	nval    int
	nblk    int
	ctx     string // closure context parameter, "" if none
	curPos  token.Pos
	failed  bool
	blkName map[*ssa.BasicBlock]string
}

// function emits fn. shared functions get linkonce_odr linkage.
func (m *moduleGen) function(fn *ssa.Function, shared bool) {
	sym := m.p.symbol(fn)
	if len(fn.Blocks) == 0 {
		// Body-less: an external symbol provided by another object (the C
		// runtime core, or assembly in the stock library).
		return
	}
	m.defined[sym] = true
	f := &funcGen{
		m:       m,
		fn:      fn,
		tm:      m.tm,
		vals:    map[ssa.Value]string{},
		blkName: map[*ssa.BasicBlock]string{},
	}
	f.b = &f.body
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*Error); ok {
				m.errorf(e)
				return
			}
			panic(fmt.Sprintf("%s: %v", fn, r))
		}
	}()
	f.gen(sym, shared)
}

func (f *funcGen) fail(pos token.Pos, format string, args ...any) {
	if !pos.IsValid() {
		pos = f.curPos
	}
	if !pos.IsValid() {
		pos = f.fn.Pos()
	}
	panic(f.m.p.errorf(pos, "%s: "+format, append([]any{f.fn.String()}, args...)...))
}

func (f *funcGen) gen(sym string, shared bool) {
	fn := f.fn
	params, ret := f.m.signature(fn)
	var head strings.Builder
	head.WriteString("  llvm.func ")
	if shared {
		head.WriteString("linkonce_odr ")
	}
	head.WriteString(mlir.Symbol(sym))
	head.WriteString("(")
	argNames := make([]string, len(params))
	for i, t := range params {
		argNames[i] = fmt.Sprintf("%%a%d", i)
		if i > 0 {
			head.WriteString(", ")
		}
		fmt.Fprintf(&head, "%s: %s", argNames[i], t)
	}
	head.WriteString(")")
	if ret != "" {
		head.WriteString(" -> " + ret)
	}
	attrs := []string{"no_unwind"}
	if a := f.m.funcAttrs(fn, sym); a != "" {
		attrs = append(attrs, a)
	}
	head.WriteString(" attributes {" + strings.Join(attrs, ", ") + "} {\n")

	for i, p := range fn.Params {
		f.vals[p] = argNames[i]
	}
	if len(fn.FreeVars) > 0 {
		f.ctx = argNames[len(argNames)-1]
	}
	for i, b := range fn.Blocks {
		f.blkName[b] = fmt.Sprintf("^bb%d", i)
	}
	if fn.Recover != nil {
		f.fail(fn.Pos(), "defer/recover is not supported yet")
	}
	// Free variables: load from the closure object, after the code pointer.
	if f.ctx != "" {
		fields := []string{mlir.Ptr}
		for _, fv := range fn.FreeVars {
			fields = append(fields, f.tm.memType(fv.Type()))
		}
		st := mlir.Struct(fields...)
		for i, fv := range fn.FreeVars {
			p := f.gep(f.ctx, st, 0, i+1)
			f.vals[fv] = f.load(p, fv.Type())
		}
	}

	// Phis are block arguments; name them all first because branches
	// may pass them before their block is emitted.
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if phi, ok := instr.(*ssa.Phi); ok {
				f.vals[phi] = f.newVal()
			}
		}
	}
	// Emit in dominator tree preorder so every value is defined before it
	// is used in the text.
	order := fn.DomPreorder()
	seen := map[*ssa.BasicBlock]bool{}
	for _, b := range order {
		seen[b] = true
	}
	for _, b := range fn.Blocks {
		if !seen[b] {
			order = append(order, b) // unreachable from entry (recover block)
		}
	}
	for i, b := range order {
		if i > 0 || len(b.Preds) > 0 {
			f.blockHeader(b, i == 0)
		}
		for _, instr := range b.Instrs {
			f.curPos = instr.Pos()
			f.instr(instr)
		}
	}
	var out strings.Builder
	out.WriteString(head.String())
	// Prologue (free variable loads were written to body; allocas go first).
	if len(fn.Blocks[0].Preds) > 0 {
		// The entry block of an MLIR region cannot have predecessors.
		out.WriteString(f.pro.String())
		// The body starts with the free variable loads, then block 0 header.
		out.WriteString(f.body.String())
	} else {
		out.WriteString(f.pro.String())
		out.WriteString(f.body.String())
	}
	out.WriteString(f.cold.String())
	out.WriteString("  }\n")
	f.m.defs.WriteString(out.String())
}

// blockHeader starts block b. For block 0 with predecessors the entry
// code branches to it first.
func (f *funcGen) blockHeader(b *ssa.BasicBlock, entry bool) {
	if entry {
		f.emit("llvm.br %s", f.blkName[b])
	}
	var args []string
	for _, instr := range b.Instrs {
		phi, ok := instr.(*ssa.Phi)
		if !ok {
			break
		}
		name := f.vals[phi]
		args = append(args, fmt.Sprintf("%s: %s", name, f.tm.valType(phi.Type())))
	}
	if len(args) > 0 {
		fmt.Fprintf(f.b, "  %s(%s):\n", f.blkName[b], strings.Join(args, ", "))
	} else {
		fmt.Fprintf(f.b, "  %s:\n", f.blkName[b])
	}
}

// ---- emission helpers ----

func (f *funcGen) newVal() string {
	f.nval++
	return fmt.Sprintf("%%v%d", f.nval)
}

func (f *funcGen) newBlock() string {
	f.nblk++
	return fmt.Sprintf("^x%d", f.nblk)
}

func (f *funcGen) emit(format string, args ...any) {
	f.b.WriteString("    ")
	fmt.Fprintf(f.b, format, args...)
	f.b.WriteByte('\n')
}

// op emits "%v = text" and returns %v.
func (f *funcGen) op(format string, args ...any) string {
	v := f.newVal()
	f.b.WriteString("    ")
	f.b.WriteString(v)
	f.b.WriteString(" = ")
	fmt.Fprintf(f.b, format, args...)
	f.b.WriteByte('\n')
	return v
}

func (f *funcGen) constInt(v int64, t string) string {
	return f.op("llvm.mlir.constant(%s : %s) : %s", mlir.IntAttr(uint64(v), intBits(t)), t, t)
}

func (f *funcGen) zero(t string) string {
	return f.op("llvm.mlir.zero : %s", t)
}

func (f *funcGen) null() string { return f.zero(mlir.Ptr) }

func (f *funcGen) undef(t string) string { return f.op("llvm.mlir.undef : %s", t) }

func (f *funcGen) addressOf(sym string) string {
	return f.op("llvm.mlir.addressof %s : !llvm.ptr", mlir.Symbol(sym))
}

// gep computes &base.(st)[0].idx... with constant indices.
func (f *funcGen) gep(base, st string, idx ...int) string {
	var s []string
	for _, i := range idx {
		s = append(s, fmt.Sprint(i))
	}
	return f.op("llvm.getelementptr inbounds %s[%s] : (!llvm.ptr) -> !llvm.ptr, %s", base, strings.Join(s, ", "), st)
}

// gepDyn computes base + i*sizeof(elem) for a dynamic i64 index.
func (f *funcGen) gepDyn(base, elem, i string) string {
	return f.op("llvm.getelementptr inbounds %s[%s] : (!llvm.ptr, i64) -> !llvm.ptr, %s", base, i, elem)
}

// gepByte computes base + off bytes.
func (f *funcGen) gepByte(base string, off int64) string {
	if off == 0 {
		return base
	}
	return f.op("llvm.getelementptr inbounds %s[%d] : (!llvm.ptr) -> !llvm.ptr, i8", base, off)
}

// load reads a Go value of type t from p and returns it in register form.
func (f *funcGen) load(p string, t types.Type) string {
	mt := f.tm.memType(t)
	v := f.op("llvm.load %s {alignment = %d : i64} : !llvm.ptr -> %s", p, f.m.p.opts.Sizes.Alignof(t), mt)
	if isBool(t) {
		return f.op("llvm.trunc %s : i8 to i1", v)
	}
	return v
}

// store writes the register value v of type t to p.
func (f *funcGen) store(v, p string, t types.Type) {
	mt := f.tm.memType(t)
	if isBool(t) {
		v = f.op("llvm.zext %s : i1 to i8", v)
	}
	f.emit("llvm.store %s, %s {alignment = %d : i64} : %s, !llvm.ptr", v, p, f.m.p.opts.Sizes.Alignof(t), mt)
}

// extract reads field i of an aggregate register value of LLVM type at,
// returning register form for Go type ft.
func (f *funcGen) extract(agg, at string, i int, ft types.Type) string {
	v := f.op("llvm.extractvalue %s[%d] : %s", agg, i, at)
	if ft != nil && isBool(ft) {
		return f.op("llvm.trunc %s : i8 to i1", v)
	}
	return v
}

func (f *funcGen) extractRaw(agg, at string, i int) string {
	return f.op("llvm.extractvalue %s[%d] : %s", agg, i, at)
}

func (f *funcGen) insert(agg, at string, i int, v string, ft types.Type) string {
	if ft != nil && isBool(ft) {
		v = f.op("llvm.zext %s : i1 to i8", v)
	}
	return f.op("llvm.insertvalue %s, %s[%d] : %s", v, agg, i, at)
}

// build makes an aggregate of LLVM type at from register values.
func (f *funcGen) build(at string, vs ...string) string {
	agg := f.undef(at)
	for i, v := range vs {
		agg = f.op("llvm.insertvalue %s, %s[%d] : %s", v, agg, i, at)
	}
	return agg
}

func (f *funcGen) makeString(ptr, n string) string { return f.build(tString, ptr, n) }

// alloca reserves stack space for a value of LLVM type t in the entry block.
func (f *funcGen) alloca(t string, align int64) string {
	v := f.newVal()
	one := f.newVal()
	fmt.Fprintf(&f.pro, "    %s = llvm.mlir.constant(1 : i64) : i64\n", one)
	fmt.Fprintf(&f.pro, "    %s = llvm.alloca %s x %s {alignment = %d : i64} : (i64) -> !llvm.ptr\n", v, one, t, align)
	return v
}

// spill stores v of Go type t in a fresh stack slot and returns its address.
func (f *funcGen) spill(v string, t types.Type) string {
	p := f.alloca(f.tm.memType(t), f.m.p.opts.Sizes.Alignof(t))
	f.store(v, p, t)
	return p
}

// branchCold emits a conditional branch: if cond is true, run the cold code
// written by fn (which must end in a terminator), else continue.
func (f *funcGen) branchCold(cond string, fn func()) {
	cold, cont := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s weights([1, 2000]), %s, %s", cond, cold, cont)
	saved := f.b
	f.b = &f.cold
	fmt.Fprintf(f.b, "  %s:\n", cold)
	fn()
	f.b = saved
	fmt.Fprintf(f.b, "  %s:\n", cont)
}

// ---- values ----

func (f *funcGen) value(v ssa.Value) string {
	if s, ok := f.vals[v]; ok {
		return s
	}
	switch v := v.(type) {
	case *ssa.Const:
		return f.constant(v.Value, v.Type())
	case *ssa.Global:
		return f.addressOf(f.m.globalRef(v))
	case *ssa.Function:
		return f.funcValue(v)
	case *ssa.Builtin:
		f.fail(v.Pos(), "builtin %s used as a value", v.Name())
	}
	f.fail(v.Pos(), "internal error: value %s (%T) used before definition", v.Name(), v)
	return ""
}

// constant materializes a Go constant of type t. c may be nil (zero value).
func (f *funcGen) constant(c constant.Value, t types.Type) string {
	vt := f.tm.valType(t)
	if c == nil {
		return f.zero(vt)
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return f.op("llvm.mlir.constant(%v) : i1", constant.BoolVal(c))
		case u.Info()&types.IsInteger != 0:
			var bits uint64
			if i, ok := constant.Int64Val(c); ok {
				bits = uint64(i)
			} else if ui, ok := constant.Uint64Val(c); ok {
				bits = ui
			} else {
				f.fail(token.NoPos, "integer constant %v overflows", c)
			}
			return f.op("llvm.mlir.constant(%s : %s) : %s", mlir.IntAttr(bits, intBits(vt)), vt, vt)
		case u.Info()&types.IsFloat != 0:
			fv, _ := constant.Float64Val(constant.ToFloat(c))
			return f.op("llvm.mlir.constant(%s : %s) : %s", mlir.FloatAttr(fv, vt), vt, vt)
		case u.Info()&types.IsComplex != 0:
			et := mlir.F64
			if u.Kind() == types.Complex64 {
				et = mlir.F32
			}
			re, _ := constant.Float64Val(constant.ToFloat(constant.Real(c)))
			im, _ := constant.Float64Val(constant.ToFloat(constant.Imag(c)))
			r := f.op("llvm.mlir.constant(%s : %s) : %s", mlir.FloatAttr(re, et), et, et)
			i := f.op("llvm.mlir.constant(%s : %s) : %s", mlir.FloatAttr(im, et), et, et)
			return f.build(vt, r, i)
		case u.Info()&types.IsString != 0:
			s := constant.StringVal(c)
			if s == "" {
				return f.zero(tString)
			}
			p := f.addressOf(f.m.stringData(s))
			return f.makeString(p, f.constInt(int64(len(s)), mlir.I64))
		case u.Kind() == types.UnsafePointer:
			// uintptr constant converted to unsafe.Pointer.
			i, _ := constant.Uint64Val(c)
			n := f.constInt(int64(i), mlir.I64)
			return f.op("llvm.inttoptr %s : i64 to !llvm.ptr", n)
		}
	}
	f.fail(token.NoPos, "unsupported constant %v of type %v", c, t)
	return ""
}

// funcValue returns a func value (pointer to a closure object) for a
// function used as a value: a constant closure with no free variables whose
// code is a wrapper taking the hidden closure parameter.
func (f *funcGen) funcValue(fn *ssa.Function) string {
	sym := f.m.funcValueSym(fn)
	return f.addressOf(sym)
}

// ---- instructions ----

func (f *funcGen) instr(instr ssa.Instruction) {
	switch v := instr.(type) {
	case *ssa.DebugRef:
	case *ssa.Phi:
		// Block argument, bound in blockHeader.
	case *ssa.Jump:
		f.emit("llvm.br %s", f.succ(v.Block(), 0))
	case *ssa.If:
		c := f.value(v.Cond)
		f.emit("llvm.cond_br %s, %s, %s", c, f.succ(v.Block(), 0), f.succ(v.Block(), 1))
	case *ssa.Return:
		f.ret(v)
	case *ssa.Panic:
		x := f.value(v.X)
		f.rtcall("gopanic", x)
		f.emit("llvm.unreachable")
	case *ssa.Alloc:
		f.alloc(v)
	case *ssa.BinOp:
		f.vals[v] = f.binop(v.Op, v.X, v.Y, v.X.Type())
	case *ssa.UnOp:
		f.vals[v] = f.unop(v)
	case *ssa.Call:
		r := f.call(v.Common(), v)
		if r != "" {
			f.vals[v] = r
		}
	case *ssa.Store:
		addr := f.value(v.Addr)
		if needsNilCheck(v.Addr) {
			f.nilCheck(addr)
		}
		f.store(f.value(v.Val), addr, v.Val.Type())
	case *ssa.Extract:
		tt := f.tm.valType(v.Tuple.Type())
		f.vals[v] = f.extractRaw(f.value(v.Tuple), tt, v.Index)
	case *ssa.FieldAddr:
		f.fieldAddr(v)
	case *ssa.Field:
		st := v.X.Type().Underlying().(*types.Struct)
		f.vals[v] = f.extract(f.value(v.X), f.tm.memType(v.X.Type()), v.Field, st.Field(v.Field).Type())
	case *ssa.IndexAddr:
		f.indexAddr(v)
	case *ssa.Index:
		f.index(v)
	case *ssa.Slice:
		f.slice(v)
	case *ssa.Convert:
		f.vals[v] = f.convert(f.value(v.X), v.X.Type(), v.Type())
	case *ssa.ChangeType:
		f.vals[v] = f.value(v.X)
	case *ssa.MakeClosure:
		f.makeClosure(v)
	case *ssa.SliceToArrayPointer:
		f.sliceToArrayPointer(v)
	case *ssa.MakeSlice:
		f.makeSlice(v)
	case *ssa.MakeInterface:
		f.makeInterface(v)
	case *ssa.TypeAssert:
		f.typeAssert(v)
	case *ssa.ChangeInterface:
		f.changeInterface(v)
	default:
		f.fail(instr.Pos(), "unsupported instruction %T: %v", instr, instr)
	}
}

// succ returns the branch target text for successor i of b, with the
// block arguments for the successor's phis.
func (f *funcGen) succ(b *ssa.BasicBlock, i int) string {
	s := b.Succs[i]
	predIdx := -1
	for j, p := range s.Preds {
		if p == b {
			predIdx = j
			break
		}
	}
	var args, types []string
	for _, instr := range s.Instrs {
		phi, ok := instr.(*ssa.Phi)
		if !ok {
			break
		}
		args = append(args, f.value(phi.Edges[predIdx]))
		types = append(types, f.tm.valType(phi.Type()))
	}
	if len(args) == 0 {
		return f.blkName[s]
	}
	return fmt.Sprintf("%s(%s : %s)", f.blkName[s], strings.Join(args, ", "), strings.Join(types, ", "))
}

func (f *funcGen) ret(r *ssa.Return) {
	switch len(r.Results) {
	case 0:
		f.emit("llvm.return")
	case 1:
		v := f.value(r.Results[0])
		f.emit("llvm.return %s : %s", v, f.tm.valType(r.Results[0].Type()))
	default:
		rt := f.tm.valType(f.fn.Signature.Results())
		vs := make([]string, len(r.Results))
		for i, x := range r.Results {
			vs[i] = f.value(x)
		}
		agg := f.build(rt, vs...)
		f.emit("llvm.return %s : %s", agg, rt)
	}
}

func needsNilCheck(addr ssa.Value) bool {
	switch addr.(type) {
	case *ssa.Alloc, *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Global:
		return false
	}
	return true
}

func (f *funcGen) nilCheck(p string) {
	n := f.null()
	c := f.op("llvm.icmp \"eq\" %s, %s : !llvm.ptr", p, n)
	f.branchCold(c, func() {
		f.rtcall("panicnil")
		f.emit("llvm.unreachable")
	})
}

func (f *funcGen) alloc(v *ssa.Alloc) {
	et := v.Type().(*types.Pointer).Elem()
	if v.Heap {
		f.vals[v] = f.newObject(et)
		return
	}
	mt := f.tm.memType(et)
	p := f.alloca(mt, f.m.p.opts.Sizes.Alignof(et))
	z := f.zero(mt)
	f.emit("llvm.store %s, %s {alignment = %d : i64} : %s, !llvm.ptr", z, p, f.m.p.opts.Sizes.Alignof(et), mt)
	f.vals[v] = p
}

// newObject allocates a zeroed heap object of type t.
func (f *funcGen) newObject(t types.Type) string {
	return f.rtcall("newobject", f.typeDesc(t))
}

func (f *funcGen) fieldAddr(v *ssa.FieldAddr) {
	x := f.value(v.X)
	f.nilCheck(x)
	st := v.X.Type().Underlying().(*types.Pointer).Elem()
	f.vals[v] = f.gep(x, f.tm.memType(st), 0, v.Field)
}

func (f *funcGen) unop(v *ssa.UnOp) string {
	x := f.value(v.X)
	t := v.X.Type()
	vt := f.tm.valType(t)
	switch v.Op {
	case token.NOT:
		one := f.op("llvm.mlir.constant(true) : i1")
		return f.op("llvm.xor %s, %s : i1", x, one)
	case token.SUB:
		switch {
		case isInteger(t):
			z := f.constInt(0, vt)
			return f.op("llvm.sub %s, %s : %s", z, x, vt)
		case isFloat(t):
			return f.op("llvm.fneg %s : %s", x, vt)
		case isComplex(t):
			et := complexElem(vt)
			re := f.op("llvm.fneg %s : %s", f.extractRaw(x, vt, 0), et)
			im := f.op("llvm.fneg %s : %s", f.extractRaw(x, vt, 1), et)
			return f.build(vt, re, im)
		}
	case token.XOR:
		m1 := f.constInt(-1, vt)
		return f.op("llvm.xor %s, %s : %s", x, m1, vt)
	case token.MUL:
		if needsNilCheck(v.X) {
			f.nilCheck(x)
		}
		return f.load(x, v.Type())
	case token.ARROW:
		f.fail(v.Pos(), "channel receive is not supported yet")
	}
	f.fail(v.Pos(), "unsupported unary op %s on %v", v.Op, t)
	return ""
}

func complexElem(vt string) string {
	if strings.Contains(vt, "f32") {
		return mlir.F32
	}
	return mlir.F64
}

// rtcall calls runtime.name with register values and returns the result
// ("" for no result).
func (f *funcGen) rtcall(name string, args ...string) string {
	fn := f.m.p.runtimeFunc(name)
	if fn == nil {
		f.fail(token.NoPos, "runtime function %s is missing from lib/src/runtime", name)
	}
	return f.directCall(fn, args)
}

// directCall calls fn with the given register arguments.
func (f *funcGen) directCall(fn *ssa.Function, args []string) string {
	sym := f.m.funcRef(fn)
	params, ret := f.m.signature(fn)
	if len(params) != len(args) {
		f.fail(token.NoPos, "internal error: call to %s with %d args, want %d", sym, len(args), len(params))
	}
	sig := fmt.Sprintf("(%s) -> %s", strings.Join(params, ", "), retOrVoid(ret))
	if ret == "" {
		f.emit("llvm.call %s(%s) : %s", mlir.Symbol(sym), strings.Join(args, ", "), sig)
		return ""
	}
	return f.op("llvm.call %s(%s) : %s", mlir.Symbol(sym), strings.Join(args, ", "), sig)
}

func retOrVoid(r string) string {
	if r == "" {
		return "()"
	}
	return r
}
