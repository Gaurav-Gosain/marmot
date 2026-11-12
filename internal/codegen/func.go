package codegen

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"os"
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
	lastPos token.Pos // last valid curPos (locations)
	failed  bool
	blkName map[*ssa.BasicBlock]string

	frame             string // _deferframe address, "" if none
	noinlineCalls     bool   // mark calls no_inline (defer thunks)
	curBlock          *ssa.BasicBlock
	fvValue           map[ssa.Value]bool // free variables captured by value
	open              *openDefers        // open-coded defers, nil if not used
	stackOK           map[ssa.Value]bool
	region            string                            // region clone: the rt_region parameter
	regionCalls       map[ssa.Instruction]*regionPlan   // calls that use a region clone
	regionFree        map[ssa.Instruction][]*regionPlan // free these regions after the instruction
	frameCalls        map[ssa.Instruction]bool          // calls that allocate into the frame region
	frameSlot         string                            // holds the frame region (nil until first use)         // heap Allocs and closures that live in the frame
	reuseAppend       map[ssa.Instruction]bool          // appends whose old array can be freed on growth
	reuseNow          bool
	appendElems       map[*ssa.Slice][]ssa.Value // append(s, a, b): elements by varargs slice
	appendMake        map[*ssa.MakeSlice]bool    // append(s, make([]T, n)...): no temporary
	skipInstr         map[ssa.Instruction]bool   // instructions folded into others
	forceUnsafe       bool                       // the access goes through a locally punned pointer
	implicitRunDefers bool
	curArgs           []ssa.Value                           // SSA arguments of a call being expanded as an intrinsic
	volatile          map[ssa.Value]bool                    // stack slots that live across setjmp
	sunk              map[ssa.Instruction]bool              // loads moved after calls (evalorder.go)
	sinkBefore        map[ssa.Instruction][]ssa.Instruction // emit these sunk loads before the instruction
	sunkDone          map[ssa.Instruction]bool
	wrapOld           string                         // recoverWrapEnter result (recoverwrap.go)
	deferThunk        bool                           // emitting a defer thunk: a builtin call here is the deferred call itself
	constOperands     bool                           // the binary op being emitted has two constant operands
	fuseLookup        map[*ssa.Lookup]*ssa.MapUpdate // map read-modify-write pairs (mapfuse.go)
	fuseSlot          map[*ssa.MapUpdate]string
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
	name := "compiler generated code"
	if f.fn != nil {
		name = f.fn.String()
		if !pos.IsValid() {
			pos = f.curPos
		}
		if !pos.IsValid() {
			pos = f.fn.Pos()
		}
	}
	panic(f.m.p.errorf(pos, "%s: "+format, append([]any{name}, args...)...))
}

func (f *funcGen) gen(sym string, shared bool) {
	fn := f.fn
	params, ret := f.m.signature(fn)
	if f.region != "" {
		params = append(params, mlir.Ptr)
	}
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
	if (callsRecover(fn) || f.m.p.isRecoverWrapper(fn)) && f.m.p.pragmas[fn]&pragmaNoinline == 0 {
		attrs = append(attrs, "no_inline")
	}
	if a := f.m.funcAttrs(fn, sym); a != "" {
		attrs = append(attrs, a)
	}
	head.WriteString(" attributes {" + strings.Join(attrs, ", ") + "} {\n")

	for i, p := range fn.Params {
		f.vals[p] = argNames[i]
	}
	if f.region != "" {
		f.region = argNames[len(argNames)-1]
	}
	if len(fn.FreeVars) > 0 {
		f.ctx = argNames[len(argNames)-1]
	}
	for i, b := range fn.Blocks {
		f.blkName[b] = fmt.Sprintf("^bb%d", i)
	}
	// Free variables: load from the closure object, after the code pointer.
	if f.ctx != "" {
		byv := f.m.p.byValueFreeVars(fn)
		fvt := make([]types.Type, len(fn.FreeVars))
		fields := []string{mlir.Ptr}
		for i, fv := range fn.FreeVars {
			fvt[i] = fv.Type()
			if byv[i] {
				fvt[i] = fv.Type().(*types.Pointer).Elem()
				if f.fvValue == nil {
					f.fvValue = map[ssa.Value]bool{}
				}
				f.fvValue[fv] = true
			}
			fields = append(fields, f.tm.memType(fvt[i]))
		}
		st := mlir.Struct(fields...)
		for i, fv := range fn.FreeVars {
			p := f.gep(f.ctx, st, 0, i+1)
			f.vals[fv] = f.load(p, fvt[i])
		}
	}

	f.findAppendElems()
	f.findAppendMake()
	f.stackOK = f.m.p.stackAllocs(fn)
	f.findRegionCalls()
	f.findFrameRegionCalls()
	f.findReusableAppends()
	if od := openDefersFor(fn); od != nil && os.Getenv("MARMOT_NO_OPENDEFER") == "" {
		f.open = od
		f.setupOpenDefers()
	} else if needsFrame(fn) {
		f.volatile = volatileAllocs(fn, f.stackOK)
		f.setupFrame()
		f.implicitRunDefers = !hasRunDefers(fn)
	}
	if f.m.p.isRecoverWrapper(fn) {
		f.wrapOld = f.recoverWrapEnter()
	}
	if !f.evalOrderOn() {
		f.computeSinks() // the older load sinking while the scheduler is off
	}
	f.sinkAllocs()
	f.findMapFusions()
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
	var ranges [][2]token.Pos
	sched := f.evalOrderOn()
	if sched {
		ranges = f.statementRanges()
	}
	for i, b := range order {
		instrs := b.Instrs
		if sched {
			instrs = f.schedule(b, ranges)
		}
		if i > 0 || len(b.Preds) > 0 {
			f.blockHeader(b, i == 0)
		}
		if b == fn.Recover && f.frame != "" {
			f.rtcall("deferreturn", f.frame)
		}
		for _, instr := range instrs {
			if f.m.static != nil && f.m.static.skip[instr] || f.skipInstr[instr] || f.sunk[instr] {
				continue
			}
			f.curBlock = b
			f.emitSunk(instr)
			f.curPos = instr.Pos()
			f.instr(instr)
			for _, pl := range f.regionFree[instr] {
				f.emit("llvm.call @rt_region_free(%s) : (!llvm.ptr) -> ()", pl.r)
			}
		}
	}
	out := &f.m.defs
	out.WriteString(head.String())
	// Prologue (allocas) first; the body starts with the free variable
	// loads, then (when block 0 has predecessors, which the entry block of
	// an MLIR region cannot have) the branch to block 0's header.
	out.WriteString(f.pro.String())
	out.WriteString(f.body.String())
	out.WriteString(f.cold.String())
	out.WriteString("  }" + f.m.loc(fn.Pos()) + "\n")
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
	f.b.WriteString(f.opLoc())
	f.b.WriteByte('\n')
}

// op emits "%v = text" and returns %v.
func (f *funcGen) op(format string, args ...any) string {
	v := f.newVal()
	f.b.WriteString("    ")
	f.b.WriteString(v)
	f.b.WriteString(" = ")
	fmt.Fprintf(f.b, format, args...)
	f.b.WriteString(f.opLoc())
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
func (f *funcGen) load(p string, t types.Type, volatile ...bool) string {
	if f.tm.isBig(t) {
		tmp := f.tempFor(t)
		f.memmove(tmp, p, f.m.p.opts.Sizes.Sizeof(t))
		return tmp
	}
	mt := f.tm.memType(t)
	vol := ""
	if len(volatile) > 0 && volatile[0] {
		vol = "volatile "
	}
	v := f.op("llvm.load %s%s {alignment = %d : i64%s} : !llvm.ptr -> %s", vol, p, f.m.p.opts.Sizes.Alignof(t), f.unsafeAttr(t), mt)
	if isBool(t) {
		return f.op("llvm.trunc %s : i8 to i1", v)
	}
	return v
}

// store writes the register value v of type t to p.
func (f *funcGen) store(v, p string, t types.Type, volatile ...bool) {
	if f.tm.isBig(t) {
		f.memmove(p, v, f.m.p.opts.Sizes.Sizeof(t))
		return
	}
	mt := f.tm.memType(t)
	if isBool(t) {
		v = f.op("llvm.zext %s : i1 to i8", v)
	}
	vol := ""
	if len(volatile) > 0 && volatile[0] {
		vol = "volatile "
	}
	f.emit("llvm.store %s%s, %s {alignment = %d : i64%s} : %s, !llvm.ptr", vol, v, p, f.m.p.opts.Sizes.Alignof(t), f.unsafeAttr(t), mt)
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
// The slot must only be read. Big values already live in memory.
func (f *funcGen) spill(v string, t types.Type) string {
	if f.tm.isBig(t) {
		return v
	}
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
		if f.m.p.opts.Sizes.Sizeof(v.Type().(*types.Pointer).Elem()) == 0 {
			return f.addressOf(f.m.zeroBase()) // all zero sized variables share an address
		}
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
		return f.zeroOf(t)
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return f.op("llvm.mlir.constant(%v) : i1", constant.BoolVal(c))
		case u.Info()&types.IsInteger != 0:
			var bits uint64
			c = constant.ToInt(c) // an integer value may arrive as a Float constant (1e2)
			if c.Kind() != constant.Int {
				f.fail(token.NoPos, "integer constant %v is not an integer", c)
			}
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
			i, _ := constant.Uint64Val(constant.ToInt(c))
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
		if f.goOps() {
			f.emit("go.panic %s", x)
			break
		}
		f.rtcall("gopanic", x)
		f.emit("llvm.unreachable")
	case *ssa.Alloc:
		f.alloc(v)
	case *ssa.BinOp:
		// gc folds constant complex products at compile time, unfused.
		_, cx := v.X.(*ssa.Const)
		_, cy := v.Y.(*ssa.Const)
		f.constOperands = cx && cy
		f.vals[v] = f.binop(v.Op, v.X, v.Y, v.X.Type())
		f.constOperands = false
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
		f.forceUnsafe = f.m.p.localPun[v.Addr]
		f.store(f.value(v.Val), addr, v.Val.Type(), f.volatile[v.Addr])
		f.forceUnsafe = false
	case *ssa.Extract:
		tt := f.tm.valType(v.Tuple.Type())
		f.vals[v] = f.extractRaw(f.value(v.Tuple), tt, v.Index)
	case *ssa.FieldAddr:
		f.fieldAddr(v)
	case *ssa.Field:
		st := v.X.Type().Underlying().(*types.Struct)
		ft := st.Field(v.Field).Type()
		if f.tm.isBig(v.X.Type()) {
			p := f.gep(f.value(v.X), f.tm.memType(v.X.Type()), 0, v.Field)
			f.vals[v] = f.load(p, ft)
		} else {
			f.vals[v] = f.extract(f.value(v.X), f.tm.memType(v.X.Type()), v.Field, ft)
		}
	case *ssa.IndexAddr:
		f.indexAddr(v)
	case *ssa.Index:
		f.index(v)
	case *ssa.Slice:
		f.slice(v)
	case *ssa.Convert:
		if bs, ok := v.Type().Underlying().(*types.Slice); ok && isString(v.X.Type()) && !isRuneElem(bs.Elem()) && readOnlyBytes(v) {
			x := f.value(v.X)
			n := f.extractRaw(x, tString, 1)
			f.vals[v] = f.build(tSlice, f.extractRaw(x, tString, 0), n, n) // no copy
			break
		}
		if bs, ok := v.X.Type().Underlying().(*types.Slice); ok && isString(v.Type()) && !isRuneElem(bs.Elem()) && tempString(v) {
			x := f.value(v.X)
			f.vals[v] = f.makeString(f.extractRaw(x, tSlice, 0), f.extractRaw(x, tSlice, 1)) // no copy
			break
		}
		if f.stackConvert(v) {
			f.reportAlloc(v, "convert", nil, "stack")
			break
		}
		if isString(v.X.Type()) != isString(v.Type()) && (isString(v.X.Type()) || isString(v.Type())) {
			f.reportAlloc(v, "convert", nil, "heap")
		}
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
	case *ssa.Defer:
		if f.open != nil {
			f.openDefer(v)
		} else {
			f.deferStmt(v)
		}
	case *ssa.RunDefers:
		if f.open != nil {
			f.openRunDefers()
		} else {
			f.rtcall("deferreturn", f.frame)
		}
	case *ssa.Go:
		f.goStmt(v)
	case *ssa.MakeChan:
		f.reportAlloc(v, "make-chan", nil, "heap")
		et := v.Type().Underlying().(*types.Chan).Elem()
		size := f.toInt64(f.value(v.Size), v.Size.Type())
		if f.goOps() {
			f.vals[v] = f.op("go.make_chan %s, %s", mlir.Symbol(f.m.typeDescSym(et)), size)
		} else {
			f.vals[v] = f.rtcall("makechan", f.typeDesc(et), size)
		}
	case *ssa.Send:
		f.send(v)
	case *ssa.Select:
		f.selectStmt(v)
	case *ssa.MakeMap:
		f.makeMap(v)
	case *ssa.Lookup:
		f.lookup(v)
	case *ssa.MapUpdate:
		f.mapUpdate(v)
	case *ssa.Range:
		f.rangeStmt(v)
	case *ssa.Next:
		f.next(v)
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
	f.freeFrameRegion()
	if f.wrapOld != "" {
		f.rtcall("recoverWrapExit", f.wrapOld)
	}
	if f.implicitRunDefers && r.Block() != f.fn.Recover {
		// The function only hands its defer stack to range-over-func
		// bodies; SSA has no RunDefers for it.
		f.rtcall("deferreturn", f.frame)
	}
	switch len(r.Results) {
	case 0:
		f.emit("llvm.return")
	case 1:
		v := f.escapeBig(f.value(r.Results[0]), r.Results[0].Type())
		f.emit("llvm.return %s : %s", v, f.tm.valType(r.Results[0].Type()))
	default:
		rt := f.tm.valType(f.fn.Signature.Results())
		vs := make([]string, len(r.Results))
		for i, x := range r.Results {
			vs[i] = f.escapeBig(f.value(x), x.Type())
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
	if f.goOps() {
		f.emit("go.nil_check %s", p)
		return
	}
	n := f.null()
	c := f.op("llvm.icmp \"eq\" %s, %s : !llvm.ptr", p, n)
	f.branchCold(c, func() {
		f.rtcall("panicnil")
		f.emit("llvm.unreachable")
	})
}

// maxAllocSize is the runtime maxAlloc (1<<47).
const maxAllocSize = 1 << 47

func (f *funcGen) alloc(v *ssa.Alloc) {
	et := v.Type().(*types.Pointer).Elem()
	if v.Comment == "makeslice" && f.m.p.opts.Sizes.Sizeof(et) > maxAllocSize {
		// make([]T, n) with a constant n that x/tools turned into an array
		// too large to allocate: gc panics (recoverable) in makeslice.
		f.rtcall("panicmakeslicelen")
		f.vals[v] = f.null()
		return
	}
	if sym, ok := f.m.static.runtimeObj(v); ok {
		if v.Heap {
			f.reportAlloc(v, allocKind(v), et, "static")
		}
		f.vals[v] = f.addressOf(sym)
		return
	}
	if v.Heap && !f.stackOK[v] && f.region != "" {
		f.reportAlloc(v, allocKind(v), et, "region")
		size := f.m.p.opts.Sizes.Sizeof(et)
		if size == 0 {
			f.vals[v] = f.addressOf(f.m.zeroBase())
			return
		}
		f.vals[v] = f.op("llvm.call @rt_region_alloc(%s, %s) : (!llvm.ptr, i64) -> !llvm.ptr", f.region, f.constInt(size, mlir.I64))
		return
	}
	if v.Heap && !f.stackOK[v] {
		f.reportAlloc(v, allocKind(v), et, "heap")
		f.vals[v] = f.newObject(et)
		return
	}
	mt := f.tm.memType(et)
	if f.m.p.opts.Sizes.Sizeof(et) > maxStackVar {
		f.reportAlloc(v, allocKind(v), et, "heap")
		f.vals[v] = f.newObject(et)
		return
	}
	if v.Heap {
		f.reportAlloc(v, allocKind(v), et, "stack")
	}
	p := f.alloca(mt, f.m.p.opts.Sizes.Alignof(et))
	if f.tm.isBig(et) && !f.volatile[v] {
		f.memset(p, f.m.p.opts.Sizes.Sizeof(et))
		f.vals[v] = p
		return
	}
	z := f.zero(mt)
	vol := ""
	if f.volatile[v] {
		vol = "volatile "
	}
	f.emit("llvm.store %s%s, %s {alignment = %d : i64} : %s, !llvm.ptr", vol, z, p, f.m.p.opts.Sizes.Alignof(et), mt)
	f.vals[v] = p
}

// newObject allocates a zeroed heap object of type t.
func (f *funcGen) newObject(t types.Type) string {
	if f.goOps() {
		return f.op("go.new %s, %s", mlir.Symbol(f.m.typeDescSym(t)), f.tm.memType(t))
	}
	return f.rtcall("newobject", f.typeDesc(t))
}

// unsafeAttr returns ", go.unsafe" for accesses of punned types.
func (f *funcGen) unsafeAttr(t types.Type) string {
	if f.forceUnsafe || f.m.p.isPunned(t) {
		return ", go.unsafe"
	}
	return ""
}

// envPointer reports whether v is a free variable of an anonymous function:
// the address of a captured variable, never nil.
func (f *funcGen) envPointer(v ssa.Value) bool {
	fv, ok := v.(*ssa.FreeVar)
	return ok && f.fn != nil && f.fn.Synthetic == "" && f.fn.Parent() != nil && fv.Parent() == f.fn
}

// goOps reports whether to emit go dialect ops.
func (f *funcGen) goOps() bool { return f.m.p.opts.GoDialect }

func (f *funcGen) fieldAddr(v *ssa.FieldAddr) {
	x := f.value(v.X)
	// An allocation, a global or the address of a field or element (whose
	// base was checked) is never nil.
	if needsNilCheck(v.X) && !f.envPointer(v.X) {
		f.nilCheck(x)
	}
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
		if f.fvValue[v.X] {
			return x // captured by value
		}
		if needsNilCheck(v.X) && !f.envPointer(v.X) {
			f.nilCheck(x)
		}
		f.forceUnsafe = f.m.p.localPun[v.X]
		r := f.load(x, v.Type(), f.volatile[v.X])
		f.forceUnsafe = false
		return r
	case token.ARROW:
		return f.recv(v)
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
	attr := ""
	if f.noinlineCalls {
		attr = " {no_inline}"
	}
	if ret == "" {
		f.emit("llvm.call %s(%s)%s : %s", mlir.Symbol(sym), strings.Join(args, ", "), attr, sig)
		return ""
	}
	return f.op("llvm.call %s(%s)%s : %s", mlir.Symbol(sym), strings.Join(args, ", "), attr, sig)
}

func retOrVoid(r string) string {
	if r == "" {
		return "()"
	}
	return r
}
