package codegen

import (
	"go/types"

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
// or a heap object when the copy is made in a loop (a stack slot would be
// reused while an older copy is still live) or is very large.
func (f *funcGen) tempFor(t types.Type) string {
	size := f.m.p.opts.Sizes.Sizeof(t)
	if size > maxStackVar || (f.curBlock != nil && inLoop(f.curBlock)) {
		return f.newObject(t)
	}
	return f.alloca(f.tm.memType(t), f.m.p.opts.Sizes.Alignof(t))
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
