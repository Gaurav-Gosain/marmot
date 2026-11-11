package codegen

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Defer protocol: see lib/src/runtime/panic.go.
//
// A function with defer statements (or one that hands its defer stack to a
// range-over-func body) owns a _deferframe { jmpbuf [32]uintptr } on its
// stack and calls rt_setjmp on entry. A nonzero result jumps to the SSA
// Recover block, which runs deferreturn and returns the named results.

const jmpbufWords = 32

var (
	tDefer      = mlir.Struct(mlir.Ptr, mlir.Ptr, mlir.Ptr, mlir.I8, mlir.I8) // link, fn, frame, wrapped, rangefunc
	tDeferFrame = mlir.Array(jmpbufWords, mlir.I64)
)

// needsFrame reports whether fn needs a _deferframe.
func needsFrame(fn *ssa.Function) bool {
	if fn.Recover != nil {
		return true
	}
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch v := instr.(type) {
			case *ssa.Defer, *ssa.RunDefers:
				return true
			case *ssa.Call:
				if bi, ok := v.Call.Value.(*ssa.Builtin); ok && bi.Name() == "ssa:deferstack" {
					return true
				}
			}
		}
	}
	return false
}

// callsRecover reports whether fn calls the recover builtin.
func callsRecover(fn *ssa.Function) bool {
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if c, ok := instr.(ssa.CallInstruction); ok {
				if bi, ok := c.Common().Value.(*ssa.Builtin); ok && bi.Name() == "recover" {
					return true
				}
			}
		}
	}
	return false
}

// setupFrame allocates the defer frame.
func (f *funcGen) setupFrame() {
	f.frame = f.alloca(tDeferFrame, 16)
}

// setjmp saves the context in the defer frame. A nonzero return means a
// deferred call recovered a panic. It is emitted at each defer statement
// (and where the defer stack is taken), so every value of the entry block,
// in particular the named result slots, dominates the Recover block.
func (f *funcGen) setjmp() {
	f.m.declareRaw("rt_setjmp", []string{mlir.Ptr}, mlir.I32, `passthrough = ["returns_twice"]`)
	r := f.op("llvm.call @rt_setjmp(%s) : (!llvm.ptr) -> i32", f.frame)
	z := f.constInt(0, mlir.I32)
	nz := f.op("llvm.icmp \"ne\" %s, %s : i32", r, z)
	cont := f.newBlock()
	if f.fn.Recover != nil {
		f.emit("llvm.cond_br %s weights([1, 2000]), %s, %s", nz, f.blkName[f.fn.Recover], cont)
	} else {
		// No recover block: a recovered panic resumes by running the
		// remaining defers and returning zero results.
		rec := f.newBlock()
		f.emit("llvm.cond_br %s weights([1, 2000]), %s, %s", nz, rec, cont)
		saved := f.b
		f.b = &f.cold
		fmt.Fprintf(f.b, "  %s:\n", rec)
		f.rtcall("deferreturn", f.frame)
		rt := f.tm.valType(f.fn.Signature.Results())
		if rt == "" {
			f.emit("llvm.return")
		} else {
			f.emit("llvm.return %s : %s", f.zeroOf(f.fn.Signature.Results()), rt)
		}
		f.b = saved
	}
	fmt.Fprintf(f.b, "  %s:\n", cont)
}

// volatileAllocs returns the stack slots read by the Recover block. They
// must live in memory across the setjmp, so all their accesses are volatile.
func volatileAllocs(fn *ssa.Function) map[ssa.Value]bool {
	if fn.Recover == nil {
		return nil
	}
	m := map[ssa.Value]bool{}
	for _, instr := range fn.Recover.Instrs {
		if u, ok := instr.(*ssa.UnOp); ok && u.Op == token.MUL {
			if a, ok := u.X.(*ssa.Alloc); ok && !a.Heap {
				m[a] = true
			}
		}
	}
	return m
}

// inLoop reports whether block b is part of a cycle.
func inLoop(b *ssa.BasicBlock) bool {
	seen := map[*ssa.BasicBlock]bool{}
	stack := append([]*ssa.BasicBlock(nil), b.Succs...)
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if x == b {
			return true
		}
		if seen[x] {
			continue
		}
		seen[x] = true
		stack = append(stack, x.Succs...)
	}
	return false
}

func (f *funcGen) deferStmt(v *ssa.Defer) {
	fv, wrapped := f.callClosure(&v.Call, true)
	own := v.DeferStack == nil || f.isOwnDeferStack(v.DeferStack)
	var rec string
	if inLoop(v.Block()) || !own {
		// The record must outlive this frame (or this iteration).
		rec = f.rtcall("newobject", f.typeDesc(deferRecordType))
	} else {
		rec = f.alloca(tDefer, 8)
	}
	frame := f.frame
	// Defers of a range-over-func body go to the enclosing function's
	// frame, which did its own setjmp.
	if own {
		f.setjmp()
	}
	if v.DeferStack != nil {
		frame = f.value(v.DeferStack)
	}
	w := int64(0)
	if wrapped {
		w = 1
	}
	rf := int64(0)
	if !own {
		rf = 1 // a range-over-func body deferring to the enclosing function
	}
	agg := f.build(tDefer, f.null(), fv, frame, f.constInt(w, mlir.I8), f.constInt(rf, mlir.I8))
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : %s, !llvm.ptr", agg, rec, tDefer)
	f.rtcall("deferpush", rec)
}

// deferRecordType is the Go type of a _defer record, for heap allocation.
var deferRecordType = types.NewStruct([]*types.Var{
	types.NewField(token.NoPos, nil, "link", types.Typ[types.UnsafePointer], false),
	types.NewField(token.NoPos, nil, "fn", types.Typ[types.UnsafePointer], false),
	types.NewField(token.NoPos, nil, "frame", types.Typ[types.UnsafePointer], false),
	types.NewField(token.NoPos, nil, "wrapped", types.Typ[types.Bool], false),
	types.NewField(token.NoPos, nil, "rangefunc", types.Typ[types.Bool], false),
}, nil)

func (f *funcGen) goStmt(v *ssa.Go) {
	fv, _ := f.callClosure(&v.Call, false)
	if f.goOps() {
		f.emit("go.go %s", fv)
		return
	}
	f.rtcall("newproc", fv)
}

// callClosure returns a func() value that performs call c with arguments
// evaluated now. wrapped is false when c is a plain call of a func value
// without arguments, which is used directly.
//
// For defer, a wrapped call goes through a thunk that keeps its own frame
// and does not inline the callee, so recover's frame check (the deferred
// function's caller is the thunk, whose caller is the panic loop) holds.
func (f *funcGen) callClosure(c *ssa.CallCommon, isDefer bool) (fv string, wrapped bool) {
	if !c.IsInvoke() && len(c.Args) == 0 {
		switch fn := c.Value.(type) {
		case *ssa.Builtin:
		case *ssa.Function:
			if len(fn.FreeVars) == 0 && !isDefer {
				return f.funcValue(fn), true
			}
		default:
			return f.value(c.Value), false
		}
	}
	// Captured values: the callee value (unless static or builtin), then
	// the arguments.
	var caps []ssa.Value
	switch c.Value.(type) {
	case *ssa.Builtin:
	case *ssa.Function:
		if c.Value.(*ssa.Function).FreeVars != nil {
			caps = append(caps, c.Value)
		}
	default:
		caps = append(caps, c.Value)
	}
	caps = append(caps, c.Args...)
	fields := []*types.Var{types.NewField(token.NoPos, nil, "F", types.Typ[types.UnsafePointer], false)}
	for i, x := range caps {
		fields = append(fields, types.NewField(token.NoPos, nil, fmt.Sprintf("X%d", i), x.Type(), false))
	}
	st := types.NewStruct(fields, nil)
	sym := f.m.thunk(f, c, caps, st)
	obj := f.newObject(st)
	mt := f.tm.memType(st)
	code := f.addressOf(sym)
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", code, obj)
	for i, x := range caps {
		p := f.gep(obj, mt, 0, i+1)
		f.store(f.value(x), p, x.Type())
	}
	return obj, true
}

// thunk emits a function taking a closure context that loads the captured
// values and performs call c.
func (m *moduleGen) thunk(parent *funcGen, c *ssa.CallCommon, caps []ssa.Value, st *types.Struct) string {
	m.nthunk++
	sym := fmt.Sprintf("%s$thunk%d", m.p.symbol(parent.fn), m.nthunk)
	m.defined[sym] = true
	t := &funcGen{
		m:       m,
		fn:      parent.fn,
		tm:      m.tm,
		vals:    map[ssa.Value]string{},
		blkName: map[*ssa.BasicBlock]string{},
	}
	t.b = &t.body
	t.ctx = "%ctx"
	t.noinlineCalls = true
	mt := m.tm.memType(st)
	for i, x := range caps {
		p := t.gep(t.ctx, mt, 0, i+1)
		t.vals[x] = t.load(p, x.Type())
	}
	t.call(c, nil)
	t.emit("llvm.return")
	var out strings.Builder
	fmt.Fprintf(&out, "  llvm.func internal %s(%%ctx: !llvm.ptr) attributes {no_unwind, no_inline, passthrough = [[\"disable-tail-calls\", \"true\"], [\"frame-pointer\", \"all\"]]} {\n", mlir.Symbol(sym))
	out.WriteString(t.pro.String())
	out.WriteString(t.body.String())
	out.WriteString(t.cold.String())
	out.WriteString("  }\n")
	m.defs.WriteString(out.String())
	return sym
}

// isOwnDeferStack reports whether ds is this function's own defer stack.
func (f *funcGen) isOwnDeferStack(ds ssa.Value) bool {
	for {
		switch x := ds.(type) {
		case *ssa.UnOp:
			ds = x.X
			continue
		case *ssa.Alloc:
			for _, r := range *x.Referrers() {
				if st, ok := r.(*ssa.Store); ok && st.Addr == x {
					if c, ok := st.Val.(*ssa.Call); ok {
						if b, ok := c.Call.Value.(*ssa.Builtin); ok && b.Name() == "ssa:deferstack" {
							return true
						}
					}
				}
			}
			return false
		case *ssa.Call:
			b, ok := x.Call.Value.(*ssa.Builtin)
			return ok && b.Name() == "ssa:deferstack"
		}
		return false
	}
}

// hasRunDefers reports whether fn contains RunDefers instructions.
func hasRunDefers(fn *ssa.Function) bool {
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if _, ok := instr.(*ssa.RunDefers); ok {
				return true
			}
		}
	}
	return false
}
