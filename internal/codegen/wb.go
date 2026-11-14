package codegen

import (
	"go/types"
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

var optWB = os.Getenv("MARMOT_WB") == "1"

// trackAddr: whether funcGen records address spaces and stack slots.
var trackAddr = optGCPtr || optWB

// onStack reports whether p is the address of (a part of) a stack slot.
func (f *funcGen) onStack(p string) bool { return f.stack0[p] }

// markFresh records that v is an object just allocated. Until the next
// call (a possible safepoint, where a cycle may start) or the end of the
// block, stores into it need no barrier: its old values are nil, and an
// object allocated while marking runs is already marked.
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
	return !optWB || f.onStack(p) || f.fresh[p] || len(ptrWords(t, f.m.p.opts.Sizes)) == 0
}

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
