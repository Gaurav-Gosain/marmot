package codegen

import (
	"go/types"
	"os"

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

// wbCheck emits the flag test and runs cold in a cold block that branches
// back; code after it continues in a new block.
func (f *funcGen) wbCheck(cold func()) {
	f.m.declareMarking()
	flag := f.op("llvm.load volatile %s {alignment = 1 : i64} : !llvm.ptr -> i8", f.addressOf("rt_gc_marking"))
	on := f.op("llvm.icmp \"ne\" %s, %s : i8", flag, f.constInt(0, mlir.I8))
	wb, cont := f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s weights([1, 2000]), %s, %s", on, wb, cont)
	saved := f.b
	f.b = &f.cold
	f.b.WriteString("  " + wb + ":\n")
	cold()
	f.emit("llvm.br %s", cont)
	f.b = saved
	f.b.WriteString("  " + cont + ":\n")
}

// wbStore emits the barrier for storing register value v of Go type t at p.
func (f *funcGen) wbStore(v, p string, t types.Type) {
	if !optWB || f.onStack(p) || len(ptrWords(t, f.m.p.opts.Sizes)) == 0 {
		return
	}
	if f.tm.memType(t) == tP && !f.tm.isBig(t) {
		f.wbCheck(func() {
			f.emit("llvm.call @rt_wb(%s, %s) : (%s, %s) -> ()", f.gcv(p), f.gcv(v), tP, tP)
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

// wbCopy emits the barrier for copying n bytes of Go type t from src over
// dst (big values, bulk copies).
func (f *funcGen) wbCopy(dst, src string, t types.Type, n int64) {
	if !optWB || f.onStack(dst) || len(ptrWords(t, f.m.p.opts.Sizes)) == 0 {
		return
	}
	f.wbCheck(func() { f.wbRange(dst, src, n) })
}

func (f *funcGen) wbRange(dst, src string, n int64) {
	f.emit("llvm.call @rt_wb_range(%s, %s, %s) : (%s, %s, i64) -> ()", f.gcv(dst), f.gcv(src), f.constInt(n, mlir.I64), tP, tP)
}

// declareMarking declares the runtime's flag and barrier functions.
func (m *moduleGen) declareMarking() {
	if _, ok := m.decls["rt_gc_marking"]; ok {
		return
	}
	m.decls["rt_gc_marking"] = "  llvm.mlir.global external @rt_gc_marking() {addr_space = 0 : i32} : i8\n"
	m.declareRaw("rt_wb", []string{tP, tP}, "", "")
	m.declareRaw("rt_wb_range", []string{tP, tP, mlir.I64}, "", "")
}
