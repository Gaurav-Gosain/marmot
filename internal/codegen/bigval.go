package codegen

import (
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
)

// Big struct and array values (over bigValueSize bytes) are kept in memory.
// Their register form is a pointer to a copy that is never written after it
// is made, so copies are only needed when a value is loaded from mutable
// memory (load), when it escapes the frame (return), and when it is stored.

// maxStackVar is the largest local variable or temporary kept on the stack;
// larger ones go to the heap (gc uses a similar limit).
const maxStackVar = 64 << 10

// tempFor returns memory for a copy of a big value of type t: a stack slot,
// or a heap object when the copy is very large or is made in a loop and may
// still be live when the next iteration makes the next copy in the same
// slot. tuios: eventHub.publish copied a subscriber's filter to the heap
// on every iteration (70 MB of a 50 MB flood).
func (f *funcGen) tempFor(t types.Type) string {
	size := f.m.p.opts.Sizes.Sizeof(t)
	if size > maxStackVar {
		return f.newObject(t)
	}
	if f.curBlock != nil && inLoop(f.curBlock) {
		v, ok := f.curInstr.(ssa.Value)
		if !optLoopTemp || !ok || !f.iterationLocal(v, map[ssa.Value]bool{}) {
			return f.newObject(t)
		}
	}
	return f.alloca(f.tm.memType(t), f.m.p.opts.Sizes.Alignof(t))
}

// iterationLocal reports whether the big value v (a pointer to its copy)
// dies within the loop iteration that makes it: an SSA value defined in a
// loop body reaches the next iteration only through a phi, and a deferred
// call or goroutine may run after the slot is reused. Big values derived
// from v (fields, elements, extracts, type changes) point into v's copy.
// Stores, sends, boxes and returns copy the contents.
func (f *funcGen) iterationLocal(v ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}
	seen[v] = true
	refs := v.Referrers()
	if refs == nil {
		return false
	}
	for _, r := range *refs {
		switch x := r.(type) {
		case *ssa.Phi, *ssa.Go, *ssa.Defer:
			return false
		case *ssa.Field, *ssa.Index, *ssa.Extract, *ssa.ChangeType:
			if d := x.(ssa.Value); f.tm.isBig(d.Type()) && !f.iterationLocal(d, seen) {
				return false
			}
		}
	}
	return true
}

// escapeBig makes a big value safe to return: a copy on the heap.
func (f *funcGen) escapeBig(v string, t types.Type) string {
	if !f.tm.isBig(t) {
		return v
	}
	p := f.newObject(t)
	f.memmove(p, v, f.m.p.opts.Sizes.Sizeof(t))
	return p
}

func (f *funcGen) memmove(dst, src string, n int64) {
	size := f.constInt(n, mlir.I64)
	f.emit("\"llvm.intr.memmove\"(%s, %s, %s) <{isVolatile = false}> : (!llvm.ptr, !llvm.ptr, i64) -> ()", dst, src, size)
}

func (f *funcGen) memset(dst string, n int64) {
	size := f.constInt(n, mlir.I64)
	zero := f.constInt(0, mlir.I8)
	f.emit("\"llvm.intr.memset\"(%s, %s, %s) <{isVolatile = false}> : (!llvm.ptr, i8, i64) -> ()", dst, zero, size)
}

// zeroOf returns the zero value of Go type t in register form.
func (f *funcGen) zeroOf(t types.Type) string {
	if tt, ok := t.(*types.Tuple); ok {
		if tt.Len() == 0 {
			return ""
		}
		if tt.Len() == 1 {
			return f.zeroOf(tt.At(0).Type())
		}
		vs := make([]string, tt.Len())
		for i := range vs {
			vs[i] = f.zeroOf(tt.At(i).Type())
		}
		return f.build(f.tm.valType(t), vs...)
	}
	if f.tm.isBig(t) {
		return f.addressOf(f.m.zeroGlobal(t))
	}
	return f.zero(f.tm.valType(t))
}

// zeroGlobal is a zero filled global holding the zero value of big type t.
// It is never written; it is not constant so it lands in zero fill memory.
func (m *moduleGen) zeroGlobal(t types.Type) string {
	sym := "zero:" + m.p.canonical(t)
	if !m.defined[sym] {
		mt := m.tm.memType(t)
		m.constGlobal(sym, "linkonce_odr", false, cZero(mt))
	}
	return sym
}
