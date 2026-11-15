package codegen

import (
	"fmt"
	"go/types"
	"golang.org/x/tools/go/ssa"
	"os"
	"strconv"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
)

// Write barriers for concurrent marking (MARMOT_WB=1, off by default). A
// store that may write a pointer into the heap or a global checks the
// runtime's flag and, while the collector marks, tells it the old and new
// values first (runtime/core/include/rt.h):
//
//	if (rt_gc_marking) rt_wb(slot, new); *slot = new
//
// A value of one pointer word goes to rt_wb; any other value with pointers
// is copied to a stack slot in the cold path and given to rt_wb_range, as
// are copies of big values. Stores to the function's own stack slots (and
// addresses computed from them) need no barrier: stacks are roots the
// collector scans again when marking ends.

// MARMOT_WB=1: the flag test and rt_wb above. MARMOT_WB=cards: card
// marking (runtime/core, RT_CARD_SHIFT): every store that may put a pointer
// into the heap marks its 512-byte card dirty when it is clean (a load, a
// test and a byte store, no call); the collector rescans the marked
// objects on dirty cards when marking ends. Stores to globals need no card (globals are rescanned
// as roots). wasm has no card table and uses the flag barrier.
var wbMode = os.Getenv("MARMOT_WB")

var optWB = wbMode == "1" || wbMode == "cards"

// smallObject is the largest object the collector rescans whole when a
// card it overlaps is dirty (runtime/core 1134f480: size classes up to
// 32 KiB); a larger one is rescanned per card, so a store into it marks
// the cards of its words.
const smallObject = 32 << 10

// cards reports whether this module marks cards instead of calling rt_wb.
func (m *moduleGen) cards() bool { return wbMode == "cards" && m.p.opts.GOARCH != "wasm" }

// trackAddr: whether funcGen records address spaces and stack slots.
var trackAddr = optGCPtr || optWB

// onStack reports whether p is the address of (a part of) a stack slot.
func (f *funcGen) onStack(p string) bool { return f.stack0[p] }

// markFresh records that v is an object just allocated, until the next call
// (a possible safepoint, where a cycle may start) or the end of the block.
// Storing v needs no barrier. Storing into v needs none with MARMOT_WB=1:
// its old values are nil, and the barrier on the store that overwrites a
// heap slot shades the old pointer. With cards it does need one: an object
// allocated while marking runs is marked and never scanned again unless its
// card is dirty, and cards record new values only, so a pointer moved into
// it from an overwritten heap slot and then dropped from registers would be
// lost (rtcore; tests/run/gc/card_fresh_move fails gc verify with it). A
// runtime that dirtied the cards of memory allocated during the mark would
// make it sound, but made the second stop 8x longer.
func (f *funcGen) markFresh(v string) {
	if !optWB {
		return
	}
	if f.fresh == nil {
		f.fresh = map[string]bool{}
	}
	f.fresh[v] = true
}

// noBarrier reports whether a store at p needs no barrier.
func (f *funcGen) noBarrier(p string, t types.Type) bool {
	if !optWB || f.onStack(p) || !f.m.cards() && f.fresh[p] || len(ptrWords(t, f.m.p.opts.Sizes)) == 0 {
		return true
	}
	// A global (or a part of one) is a root: no card.
	return f.m.cards() && f.addr0[p]
}

// card marks the card of address a dirty.
func (f *funcGen) card(a string) {
	if f.cardBase == "" {
		f.m.declareCards()
		base, ref := f.newVal(), f.newVal()
		fmt.Fprintf(&f.pro, "    %s = llvm.mlir.addressof @rt_card_table : !llvm.ptr\n", ref)
		fmt.Fprintf(&f.pro, "    %s = llvm.load %s {alignment = 8 : i64} : !llvm.ptr -> !llvm.ptr\n", base, ref)
		f.cardBase = f.markAddr(base)
	}
	i := f.op("llvm.ptrtoint %s : %s to i64", a, f.pty(a))
	n := f.op("llvm.lshr %s, %s : i64", i, f.constInt(cardShift, mlir.I64))
	n = f.op("llvm.and %s, %s : i64", n, f.constInt(cardMask, mlir.I64))
	c := f.op("llvm.getelementptr %s[%s] : (!llvm.ptr, i64) -> !llvm.ptr, i8", f.cardBase, n)
	// Write the card only when it is clean (rtcore 42a576a2): an
	// unconditional store to an already dirty card makes cores storing
	// near each other bounce its cache line.
	cur := f.op("llvm.load %s {alignment = 1 : i64} : !llvm.ptr -> i8", c)
	clean := f.op("llvm.icmp \"ne\" %s, %s : i8", cur, f.constInt(0, mlir.I8))
	mark, cont := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s, %s, %s", clean, mark, cont)
	f.b.WriteString("  " + mark + ":\n")
	f.emit("llvm.store %s, %s {alignment = 1 : i64} : i8, !llvm.ptr", f.constInt(0, mlir.I8), c)
	f.emit("llvm.br %s", cont)
	f.b.WriteString("  " + cont + ":\n")
}

// cardShift and cardMask are RT_CARD_SHIFT and RT_CARD_MASK: 512-byte
// cards, a 128 MiB table that repeats every 64 GiB of address space
// (rtcore 2aeb79bd; an address outside the heap dirties some heap card,
// which only costs a rescan).
const (
	cardShift = 9
	cardMask  = 1<<27 - 1
)

// wbCheck emits the flag test and runs cold in a cold block that branches
// back; code after it continues in a new block.
func (f *funcGen) wbCheck(cold func()) {
	f.m.declareMarking()
	// The flag changes only at a safepoint, and safepoints are calls: one
	// load serves every store up to the next call in the block.
	if f.wbFlag == "" {
		flag := f.op("llvm.load volatile %s {alignment = 1 : i64} : !llvm.ptr -> i8", f.addressOf("rt_gc_marking"))
		f.wbFlag = f.op("llvm.icmp \"ne\" %s, %s : i8", flag, f.constInt(0, mlir.I8))
	}
	on := f.wbFlag
	wb, cont := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s weights([1, 2000]), %s, %s", on, wb, cont)
	saved, savedFresh := f.b, f.fresh
	f.b = &f.cold
	f.b.WriteString("  " + wb + ":\n")
	cold()
	f.emit("llvm.br %s", cont)
	// The barrier calls in the cold block are not safepoints.
	f.b, f.fresh, f.wbFlag = saved, savedFresh, on
	f.b.WriteString("  " + cont + ":\n")
}

// wbStore emits the barrier for storing register value v of Go type t at p.
func (f *funcGen) wbStore(v, p string, t types.Type) {
	if f.noBarrier(p, t) {
		return
	}
	mt := f.tm.memType(t)
	if f.m.cards() {
		// A pointer to an object allocated since the last call needs no card
		// either: allocated while marking runs, it is already marked.
		if !f.cardSkip && !f.fresh[v] {
			f.cardStore(p, t, mt)
		}
		return
	}
	switch {
	case mt == tP && !f.tm.isBig(t):
		f.wbCheck(func() { f.wb(p, v) })
		return
	case mt == tString || mt == tSlice:
		// One pointer word, the first.
		f.wbCheck(func() { f.wb(p, f.extractRaw(v, mt, 0)) })
		return
	case mt == tIface:
		// The first word is a type descriptor (static) or an itab: static,
		// or made at run time and kept reachable by the runtime's itab
		// table for good, so never lost. Only the data word needs it.
		f.wbCheck(func() { f.wb(f.gepByte(p, 8), f.extractRaw(v, mt, 1)) })
		return
	}
	if leaves, ok := ptrLeaves(t, nil, nil); ok && !f.tm.isBig(t) {
		// A small aggregate: one rt_wb per pointer word under one check.
		f.wbCheck(func() {
			for _, l := range leaves {
				idx := make([]string, len(l))
				for i, x := range l {
					idx[i] = strconv.Itoa(x)
				}
				w := f.op("llvm.extractvalue %s[%s] : %s", v, strings.Join(idx, ", "), mt)
				f.wb(f.gep(p, mt, append([]int{0}, l...)...), w)
			}
		})
		return
	}
	size := f.m.p.opts.Sizes.Sizeof(t)
	f.wbCheck(func() {
		src := v
		if !f.tm.isBig(t) {
			src = f.alloca(f.tm.memType(t), f.m.p.opts.Sizes.Alignof(t))
			mt := f.tm.memType(t)
			if isBool(t) {
				v = f.op("llvm.zext %s : i1 to i8", v)
			}
			f.emit("llvm.store %s, %s {alignment = %d : i64} : %s, !llvm.ptr", f.asVal(v, mt), src, f.m.p.opts.Sizes.Alignof(t), mt)
		}
		f.wbRange(p, src, size)
	})
}

// maxLeaves is the most pointer words a small value gets single barriers
// for; more go to rt_wb_range.
const maxLeaves = 4

// ptrLeaves appends to out the index paths, in the LLVM type of a value of
// t, of its words that may point into the heap: pointers, the data word of
// strings and slices, the data word of interfaces. It reports false when
// there are more than maxLeaves.
func ptrLeaves(t types.Type, path []int, out [][]int) ([][]int, bool) {
	leaf := func(extra ...int) ([][]int, bool) {
		p := append(append([]int(nil), path...), extra...)
		out = append(out, p)
		return out, len(out) <= maxLeaves
	}
	switch u := t.Underlying().(type) {
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature:
		return leaf()
	case *types.Basic:
		switch {
		case u.Kind() == types.UnsafePointer:
			return leaf()
		case u.Info()&types.IsString != 0:
			return leaf(0)
		}
		return out, true
	case *types.Slice:
		return leaf(0)
	case *types.Interface:
		return leaf(1)
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			var ok bool
			if out, ok = ptrLeaves(u.Field(i).Type(), append(append([]int(nil), path...), i), out); !ok {
				return out, false
			}
		}
		return out, true
	case *types.Array:
		if u.Len() > maxLeaves {
			var none [][]int
			if inner, _ := ptrLeaves(u.Elem(), nil, none); len(inner) == 0 {
				return out, true
			}
			return out, false
		}
		for k := int64(0); k < u.Len(); k++ {
			var ok bool
			if out, ok = ptrLeaves(u.Elem(), append(append([]int(nil), path...), int(k)), out); !ok {
				return out, false
			}
		}
		return out, true
	}
	return out, true
}

// wbCopy emits the barrier for copying n bytes of Go type t from src over
// dst (big values, bulk copies).
func (f *funcGen) wbCopy(dst, src string, t types.Type, n int64) {
	if f.noBarrier(dst, t) {
		return
	}
	if f.m.cards() {
		f.cardRange(dst, n)
		return
	}
	f.wbCheck(func() { f.wbRange(dst, src, n) })
}

func (f *funcGen) wb(slot, v string) {
	f.emit("llvm.call %s@rt_wb(%s, %s) : (%s, %s) -> ()", f.m.wbConv(), f.gcv(slot), f.gcv(v), tP, tP)
}

func (f *funcGen) wbRange(dst, src string, n int64) {
	w := f.m.wbWord()
	f.emit("llvm.call %s@rt_wb_range(%s, %s, %s) : (%s, %s, %s) -> ()", f.m.wbConv(), f.gcv(dst), f.gcv(src), f.constInt(n, w), tP, tP, w)
}

// declareMarking declares the runtime's flag and barrier functions.
func (m *moduleGen) declareMarking() {
	if _, ok := m.decls["rt_gc_marking"]; ok {
		return
	}
	// hidden: the runtime is linked into the binary, so the flag is read
	// without a GOT load.
	m.decls["rt_gc_marking"] = "  llvm.mlir.global external hidden @rt_gc_marking() {addr_space = 0 : i32} : i8\n"
	// The barrier functions keep every register (preserve_all), so a call
	// on the cold path costs the hot path no saves.
	// wasm32 has no preserve_all; its uintptr_t is 32 bits.
	m.decls["rt_wb"] = "  llvm.func " + m.wbConv() + "@rt_wb(" + tP + ", " + tP + ")\n"
	m.decls["rt_wb_range"] = "  llvm.func " + m.wbConv() + "@rt_wb_range(" + tP + ", " + tP + ", " + m.wbWord() + ")\n"
}

// wbConv is the calling convention of the barrier functions: preserve_all
// (runtime/core/include/rt.h, RT_PRESERVE_ALL), the default on wasm.
func (m *moduleGen) wbConv() string {
	if m.p.opts.GOARCH == "wasm" {
		return ""
	}
	return "preserve_allcc "
}

// wbWord is the LLVM type of a C uintptr_t.
func (m *moduleGen) wbWord() string {
	if m.p.opts.GOARCH == "wasm" {
		return mlir.I32
	}
	return mlir.I64
}

// cardStore marks the cards of the pointer words a store of type t at p
// writes (before the store: the collector clears cards only while the
// world is stopped, so the order does not matter).
func (f *funcGen) cardStore(p string, t types.Type, mt string) {
	if r := f.cardRoot; r != "" {
		// A store into a small object: one mark (on any card it overlaps)
		// serves every store into it until the next call.
		if f.cardDone[r] {
			return
		}
		if f.cardDone == nil {
			f.cardDone = map[string]bool{}
		}
		f.cardDone[r] = true
		f.card(p)
		return
	}
	switch {
	case mt == tP && !f.tm.isBig(t), mt == tString, mt == tSlice:
		f.card(p)
		return
	case mt == tIface:
		f.card(f.gepByte(p, 8))
		return
	}
	if leaves, ok := ptrLeaves(t, nil, nil); ok && !f.tm.isBig(t) {
		for _, l := range leaves {
			f.card(f.gep(p, mt, append([]int{0}, l...)...))
		}
		return
	}
	f.cardRange(p, f.m.p.opts.Sizes.Sizeof(t))
}

func (f *funcGen) cardRange(dst string, n int64) {
	f.m.declareCards()
	f.emit("llvm.call preserve_allcc @rt_card_range(%s, %s) : (%s, i64) -> ()", f.gcv(dst), f.constInt(n, mlir.I64), tP)
}

// declareCards declares the card table and rt_card_range.
func (m *moduleGen) declareCards() {
	if _, ok := m.decls["rt_card_table"]; ok {
		return
	}
	m.decls["rt_card_table"] = "  llvm.mlir.global external hidden @rt_card_table() {addr_space = 0 : i32} : !llvm.ptr\n"
	m.decls["rt_card_range"] = "  llvm.func preserve_allcc @rt_card_range(" + tP + ", i64)\n"
}

// staticValue reports whether every pointer in v points at static data or
// is nil: a constant (nil, a string literal), a global's address, a
// function without free variables. Card marking records new pointers only,
// so storing such a value needs no card (the flag barrier, which also
// shades the old value, still needs one).
func staticValue(v ssa.Value) bool {
	switch x := v.(type) {
	case *ssa.Const, *ssa.Global:
		return true
	case *ssa.Function:
		return len(x.FreeVars) == 0
	}
	return false
}

// rootedType reports whether a value of type t is a *runtime.g or a
// *runtime.sudog. The collector marks every g and every sudog it can reach
// from a heap object at the second stop of a cycle (rtcore: a g stays in its
// rt_g slot, which is a root; sudogs are walked from g.sudogcache and
// g.waiting), so storing one needs no card. An unsafe.Pointer holding one
// (g.param) keeps its card. A new place that keeps a sudog only in a heap
// structure must be added to that walk first.
func rootedType(t types.Type) bool {
	p, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := p.Elem().(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != "runtime" {
		return false
	}
	return n.Obj().Name() == "g" || n.Obj().Name() == "sudog"
}

// smallRoot returns the object addr points into, by field and array
// element addresses, when its type is at most smallObject; else nil.
func (f *funcGen) smallRoot(addr ssa.Value) ssa.Value {
	for {
		switch x := addr.(type) {
		case *ssa.FieldAddr:
			addr = x.X
			continue
		case *ssa.IndexAddr:
			if _, ok := x.X.Type().Underlying().(*types.Pointer); ok {
				addr = x.X // an element of an array behind a pointer
				continue
			}
			return nil // a slice element: the backing array's size is unknown
		}
		break
	}
	pt, ok := addr.Type().Underlying().(*types.Pointer)
	if !ok || f.m.p.opts.Sizes.Sizeof(pt.Elem()) > smallObject {
		return nil
	}
	return addr
}
