package codegen

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Open-coded defers. When no deferred call of a function can recover, the
// deferred calls do not need rt_setjmp or heap closures: each defer
// statement saves its callee and arguments in a frame slot block and sets
// a bit, and the return path runs the calls directly in reverse order.
// One frame-allocated _defer record whose fn is a runner over the same
// slot block lets the runtime run the pending calls when a panic or Goexit
// unwinds through the frame (the runner clears each bit before its call,
// so no call runs twice).
//
// Conditions: at most 8 defer statements, none in a loop, none deferring
// to another function's defer stack, no range-over-func defer stack, and
// every deferred callee is known and does not call recover.

type openDefers struct {
	defers []*ssa.Defer
	index  map[*ssa.Defer]int
	caps   [][]ssa.Value // captured values per defer (callee value if dynamic, then args)
	slotT  *types.Struct // bits, then the captures of each defer
	slotMT string
	firstF []int // field index of the first capture of each defer
	slots  string
	rec    string
	clo    string
	gslot  string // the goroutine's g, saved at the push
	noRec  bool   // nothing can panic while calls are pending: no record
	runner string
}

// openDefersFor reports whether fn's defers can be open coded.
func openDefersFor(fn *ssa.Function) *openDefers {
	if fn.Recover == nil {
		return nil
	}
	od := &openDefers{index: map[*ssa.Defer]int{}}
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch x := instr.(type) {
			case *ssa.Defer:
				if x.DeferStack != nil || inLoop(b) || !deferCannotRecover(&x.Call) {
					return nil
				}
				od.defers = append(od.defers, x)
			case *ssa.Call:
				if bi, ok := x.Call.Value.(*ssa.Builtin); ok && bi.Name() == "ssa:deferstack" {
					return nil
				}
			}
		}
	}
	if len(od.defers) == 0 || len(od.defers) > 8 {
		return nil
	}
	// Index in reverse postorder of blocks, then instruction order, so a
	// defer that runs later has a higher index (the CFG region is acyclic).
	rpo := map[*ssa.BasicBlock]int{}
	for i, b := range reversePostorder(fn) {
		rpo[b] = i
	}
	pos := func(d *ssa.Defer) int {
		for i, instr := range d.Block().Instrs {
			if instr == ssa.Instruction(d) {
				return rpo[d.Block()]*100000 + i
			}
		}
		return 0
	}
	ds := od.defers
	for i := 1; i < len(ds); i++ {
		for j := i; j > 0 && pos(ds[j]) < pos(ds[j-1]); j-- {
			ds[j], ds[j-1] = ds[j-1], ds[j]
		}
	}
	fields := []*types.Var{types.NewField(token.NoPos, nil, "bits", types.Typ[types.Uint8], false)}
	for k, d := range ds {
		od.index[d] = k
		var caps []ssa.Value
		if _, ok := d.Call.Value.(*ssa.Builtin); !ok {
			if fv, ok := d.Call.Value.(*ssa.Function); !ok || len(fv.FreeVars) > 0 {
				caps = append(caps, d.Call.Value)
			}
		}
		caps = append(caps, d.Call.Args...)
		od.caps = append(od.caps, caps)
		od.firstF = append(od.firstF, len(fields))
		for i, c := range caps {
			fields = append(fields, types.NewField(token.NoPos, nil, fmt.Sprintf("d%d_%d", k, i), c.Type(), false))
		}
	}
	od.slotT = types.NewStruct(fields, nil)
	return od
}

// deferCannotRecover reports whether the deferred call is to known code
// that does not call recover.
func deferCannotRecover(c *ssa.CallCommon) bool {
	if c.IsInvoke() {
		return false
	}
	switch v := c.Value.(type) {
	case *ssa.Builtin:
		return true
	case *ssa.Function:
		return !mayRecover(v)
	case *ssa.MakeClosure:
		fn, ok := v.Fn.(*ssa.Function)
		return ok && !mayRecover(fn)
	}
	return false
}

func reversePostorder(fn *ssa.Function) []*ssa.BasicBlock {
	seen := map[*ssa.BasicBlock]bool{}
	var post []*ssa.BasicBlock
	var visit func(b *ssa.BasicBlock)
	visit = func(b *ssa.BasicBlock) {
		seen[b] = true
		for _, s := range b.Succs {
			if !seen[s] {
				visit(s)
			}
		}
		post = append(post, b)
	}
	if len(fn.Blocks) > 0 {
		visit(fn.Blocks[0])
	}
	for i, j := 0, len(post)-1; i < j; i, j = i+1, j-1 {
		post[i], post[j] = post[j], post[i]
	}
	return post
}

// setupOpenDefers allocates the slot block, record and runner closure and
// clears the bits. Called at entry.
func (f *funcGen) setupOpenDefers() {
	od := f.open
	od.slotMT = f.tm.memType(od.slotT)
	od.slots = f.alloca(od.slotMT, 8)
	od.rec = f.alloca(tDefer, 8)
	od.clo = f.alloca(mlir.Struct(mlir.Ptr, mlir.Ptr, mlir.Ptr), 8)
	od.gslot = f.alloca(mlir.Ptr, 8)
	od.noRec = !f.m.p.deferRecordNeeded(f.fn, od)
	od.runner = f.m.openDeferRunner(f, od)
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : i8, !llvm.ptr", f.constInt(0, mlir.I8), od.slots)
	code := f.addressOf(od.runner)
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", code, od.clo)
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", od.slots, f.gepByte(od.clo, 8))
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", od.rec, f.gepByte(od.clo, 16))
}

// openDefer saves the call of defer v and sets its bit; the first defer
// executed in the frame pushes the record.
func (f *funcGen) openDefer(v *ssa.Defer) {
	od := f.open
	k := od.index[v]
	for i, c := range od.caps[k] {
		f.store(f.value(c), f.gep(od.slots, od.slotMT, 0, od.firstF[k]+i), c.Type())
	}
	old := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> i8", od.slots)
	nw := f.op("llvm.or %s, %s : i8", old, f.constInt(int64(1)<<k, mlir.I8))
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : i8, !llvm.ptr", nw, od.slots)
	if od.noRec {
		return
	}
	first := f.op("llvm.icmp \"eq\" %s, %s : i8", old, f.constInt(0, mlir.I8))
	push, cont := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s, %s, %s", first, push, cont)
	fmt.Fprintf(f.b, "  %s:\n", push)
	off, ok := f.m.p.gDeferOffset()
	getg := f.m.p.runtimeFunc("getg")
	if ok && getg != nil {
		// Inline deferpush; the g is saved for the pop (a frame never
		// changes goroutines).
		gp := f.directCall(getg, nil)
		f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", gp, od.gslot)
		slot := f.gepByte(gp, off)
		link := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", slot)
		agg := f.build(tDefer, link, od.clo, od.slots, f.constInt(0, mlir.I8), f.constInt(0, mlir.I8))
		f.emit("llvm.store %s, %s {alignment = 8 : i64} : %s, !llvm.ptr", agg, od.rec, tDefer)
		f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", od.rec, slot)
	} else {
		agg := f.build(tDefer, f.null(), od.clo, od.slots, f.constInt(0, mlir.I8), f.constInt(0, mlir.I8))
		f.emit("llvm.store %s, %s {alignment = 8 : i64} : %s, !llvm.ptr", agg, od.rec, tDefer)
		f.rtcall("deferpush", od.rec)
	}
	f.emit("llvm.br %s", cont)
	fmt.Fprintf(f.b, "  %s:\n", cont)
}

// openRunDefers runs the pending deferred calls and pops the record. A
// defer whose block dominates this return ran for sure, and its callee and
// arguments are still available as SSA values: it is called directly
// (inlinable). Others are run from the slot block, guarded by their bit.
func (f *funcGen) openRunDefers() {
	od := f.open
	bits := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> i8", od.slots)
	any := f.op("llvm.icmp \"ne\" %s, %s : i8", bits, f.constInt(0, mlir.I8))
	run, cont := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s, %s, %s", any, run, cont)
	fmt.Fprintf(f.b, "  %s:\n", run)
	mt := od.slotMT
	for k := len(od.defers) - 1; k >= 0; k-- {
		d := od.defers[k]
		b := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> i8", od.slots)
		bit := f.constInt(int64(1)<<k, mlir.I8)
		cleared := f.op("llvm.and %s, %s : i8", b, f.constInt(^(int64(1)<<k)&0xff, mlir.I8))
		if f.curBlock != nil && d.Block().Dominates(f.curBlock) {
			f.emit("llvm.store %s, %s {alignment = 8 : i64} : i8, !llvm.ptr", cleared, od.slots)
			f.call(&d.Call, d)
			continue
		}
		set := f.op("llvm.and %s, %s : i8", b, bit)
		has := f.op("llvm.icmp \"ne\" %s, %s : i8", set, f.constInt(0, mlir.I8))
		yes, next := f.newBlock(), f.newBlock()
		f.emit("llvm.cond_br %s, %s, %s", has, yes, next)
		fmt.Fprintf(f.b, "  %s:\n", yes)
		f.emit("llvm.store %s, %s {alignment = 8 : i64} : i8, !llvm.ptr", cleared, od.slots)
		saved := map[ssa.Value]string{}
		for i, c := range od.caps[k] {
			saved[c] = f.vals[c]
			f.vals[c] = f.load(f.gep(od.slots, mt, 0, od.firstF[k]+i), c.Type())
		}
		f.call(&d.Call, d)
		for c, v := range saved {
			if v == "" {
				delete(f.vals, c)
			} else {
				f.vals[c] = v
			}
		}
		f.emit("llvm.br %s", next)
		fmt.Fprintf(f.b, "  %s:\n", next)
	}
	if !od.noRec {
		f.popDeferRecord(od)
	}
	f.emit("llvm.br %s", cont)
	fmt.Fprintf(f.b, "  %s:\n", cont)
}

// popDeferRecord unlinks the frame's record: gp._defer = rec.link when the
// record is on top (always, unless the runtime state is unusual, where
// deferreturn handles it). Falls back to deferreturn if the runtime's g
// has no _defer field.
func (f *funcGen) popDeferRecord(od *openDefers) {
	off, ok := f.m.p.gDeferOffset()
	getg := f.m.p.runtimeFunc("getg")
	if !ok || getg == nil {
		f.rtcall("deferreturn", od.slots)
		return
	}
	gp := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", od.gslot)
	slot := f.gepByte(gp, off)
	top := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", slot)
	mine := f.op("llvm.icmp \"eq\" %s, %s : !llvm.ptr", top, od.rec)
	fast, slow, done := f.newBlock(), f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s weights([2000, 1]), %s, %s", mine, fast, slow)
	fmt.Fprintf(f.b, "  %s:\n", fast)
	link := f.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", od.rec)
	f.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", link, slot)
	f.emit("llvm.br %s", done)
	fmt.Fprintf(f.b, "  %s:\n", slow)
	f.rtcall("deferreturn", od.slots)
	f.emit("llvm.br %s", done)
	fmt.Fprintf(f.b, "  %s:\n", done)
}

// openDeferRunner emits the runner, called by the runtime (which pops the
// record and clears its fn first) when a panic or Goexit unwinds through
// the frame: it runs the highest pending call. If calls remain, it re-pushes
// the record before the call, so a panic raised by the call still finds
// them, and the runtime calls the runner again for the next one.
func (m *moduleGen) openDeferRunner(parent *funcGen, od *openDefers) string {
	m.nthunk++
	sym := fmt.Sprintf("%s$opendefer%d", m.p.symbol(parent.fn), m.nthunk)
	m.defined[sym] = true
	t := &funcGen{m: m, fn: parent.fn, tm: m.tm, vals: map[ssa.Value]string{}, blkName: map[*ssa.BasicBlock]string{}, deferThunk: true}
	t.b = &t.body
	slots := t.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", t.gepByte("%ctx", 8))
	rec := t.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", t.gepByte("%ctx", 16))
	mt := m.tm.memType(od.slotT)
	off, haveOff := m.p.gDeferOffset()
	getg := m.p.runtimeFunc("getg")
	for k := len(od.defers) - 1; k >= 0; k-- {
		d := od.defers[k]
		bits := t.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> i8", slots)
		bit := t.constInt(int64(1)<<k, mlir.I8)
		set := t.op("llvm.and %s, %s : i8", bits, bit)
		has := t.op("llvm.icmp \"ne\" %s, %s : i8", set, t.constInt(0, mlir.I8))
		yes, next := t.newBlock(), t.newBlock()
		t.emit("llvm.cond_br %s, %s, %s", has, yes, next)
		fmt.Fprintf(t.b, "  %s:\n", yes)
		cleared := t.op("llvm.and %s, %s : i8", bits, t.constInt(^(int64(1)<<k)&0xff, mlir.I8))
		t.emit("llvm.store %s, %s {alignment = 8 : i64} : i8, !llvm.ptr", cleared, slots)
		if k > 0 {
			more := t.op("llvm.icmp \"ne\" %s, %s : i8", cleared, t.constInt(0, mlir.I8))
			push, run := t.newBlock(), t.newBlock()
			t.emit("llvm.cond_br %s, %s, %s", more, push, run)
			fmt.Fprintf(t.b, "  %s:\n", push)
			agg := t.build(tDefer, t.null(), "%ctx", slots, t.constInt(0, mlir.I8), t.constInt(0, mlir.I8))
			if haveOff && getg != nil {
				gp := t.directCall(getg, nil)
				gslot := t.gepByte(gp, off)
				link := t.op("llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr", gslot)
				agg = t.op("llvm.insertvalue %s, %s[0] : %s", link, agg, tDefer)
				t.emit("llvm.store %s, %s {alignment = 8 : i64} : %s, !llvm.ptr", agg, rec, tDefer)
				t.emit("llvm.store %s, %s {alignment = 8 : i64} : !llvm.ptr, !llvm.ptr", rec, gslot)
			} else {
				t.emit("llvm.store %s, %s {alignment = 8 : i64} : %s, !llvm.ptr", agg, rec, tDefer)
				t.rtcall("deferpush", rec)
			}
			t.emit("llvm.br %s", run)
			fmt.Fprintf(t.b, "  %s:\n", run)
		}
		t.vals = map[ssa.Value]string{}
		for i, c := range od.caps[k] {
			t.vals[c] = t.load(t.gep(slots, mt, 0, od.firstF[k]+i), c.Type())
		}
		t.call(&d.Call, d)
		t.emit("llvm.return")
		fmt.Fprintf(t.b, "  %s:\n", next)
	}
	t.emit("llvm.return")
	var out strings.Builder
	fmt.Fprintf(&out, "  llvm.func internal %s(%%ctx: !llvm.ptr) attributes {no_unwind, passthrough = [[\"frame-pointer\", \"non-leaf\"]]} {\n", mlir.Symbol(sym))
	out.WriteString(t.pro.String())
	out.WriteString(t.body.String())
	out.WriteString(t.cold.String())
	out.WriteString("  }\n")
	m.defs.WriteString(out.String())
	return sym
}
