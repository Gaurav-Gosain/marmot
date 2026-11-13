package codegen

import (
	"fmt"
	"go/constant"
	"go/types"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// Stack buffers for conversions, as gc's
// tmpBuf: string(b) and []byte(s) whose result does not escape use a
// 32 byte frame buffer when the data fits (else the runtime allocates);
// []byte(constant) that does not escape gets a frame array of its exact
// length (up to 64 KB). The result is a copy, as without the buffer. A
// conversion in a loop must not reach a phi, so a value never outlives
// the iteration that filled the buffer.

const tmpBufSize = 32

func (f *funcGen) stackConvert(v *ssa.Convert) bool {
	if !optStackConv || f.region != "" || len(f.reuseAppend) > 0 {
		return false
	}
	toBytes := false
	if bs, ok := v.Type().Underlying().(*types.Slice); ok && isString(v.X.Type()) && !isRuneElem(bs.Elem()) {
		toBytes = true
	} else if bs, ok := v.X.Type().Underlying().(*types.Slice); !ok || !isString(v.Type()) || isRuneElem(bs.Elem()) {
		return false
	}
	if f.m.p.escapes(v, map[ssa.Value]bool{}, inLoop(v.Block())) {
		return false
	}
	if toBytes {
		if c, ok := v.X.(*ssa.Const); ok && c.Value != nil && c.Value.Kind() == constant.String {
			s := constant.StringVal(c.Value)
			if len(s) == 0 || len(s) > maxStackVar {
				return false
			}
			buf := f.alloca(fmt.Sprintf("!llvm.array<%d x i8>", len(s)), 1)
			x := f.value(v.X)
			f.memmove(buf, f.extractRaw(x, tString, 0), int64(len(s)))
			n := f.constInt(int64(len(s)), mlir.I64)
			f.vals[v] = f.build(tSlice, buf, n, n)
			return true
		}
	}
	x := f.value(v.X)
	at := tSlice
	if toBytes {
		at = tString
	}
	src := f.extractRaw(x, at, 0)
	n := f.extractRaw(x, at, 1)
	buf := f.alloca(fmt.Sprintf("!llvm.array<%d x i8>", tmpBufSize), 1)
	small := f.op("llvm.icmp \"ule\" %s, %s : i64", n, f.constInt(tmpBufSize, mlir.I64))
	yes, no, join := f.newBlock(), f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s, %s, %s", small, yes, no)
	f.b.WriteString("  " + yes + ":\n")
	f.emit("\"llvm.intr.memmove\"(%s, %s, %s) <{isVolatile = false}> : (!llvm.ptr, !llvm.ptr, i64) -> ()", buf, src, n)
	f.emit("llvm.br %s(%s : !llvm.ptr)", join, buf)
	f.b.WriteString("  " + no + ":\n")
	var heap string
	if toBytes {
		heap = f.extractRaw(f.rtcall("stringtoslicebyte", x), tSlice, 0)
	} else {
		heap = f.extractRaw(f.rtcall("slicebytetostring", x), tString, 0)
	}
	f.emit("llvm.br %s(%s : !llvm.ptr)", join, heap)
	p := f.newVal()
	f.b.WriteString("  " + join + "(" + p + ": !llvm.ptr):\n")
	if toBytes {
		f.vals[v] = f.build(tSlice, p, n, n)
	} else {
		f.vals[v] = f.makeString(p, n)
	}
	return true
}

// stackMakeSlice gives make([]T, n, m) whose slice does not escape a
// zeroed 32 byte frame buffer when cap*sizeof(T) fits (gc 1.25); bad or
// larger sizes go to runtime.makeslice, which also panics as required.
func (f *funcGen) stackMakeSlice(v *ssa.MakeSlice, l, c string) bool {
	// In-place reuse frees an old backing array on growth; it must never
	// see a frame buffer.
	if !optStackConv || f.region != "" || len(f.reuseAppend) > 0 {
		return false
	}
	st := v.Type().Underlying().(*types.Slice)
	es := f.m.p.opts.Sizes.Sizeof(st.Elem())
	if es == 0 || es > tmpBufSize || f.m.p.escapes(v, map[ssa.Value]bool{}, inLoop(v.Block())) {
		return false
	}
	maxCap := int64(tmpBufSize) / es
	align := max(f.m.p.opts.Sizes.Alignof(st.Elem()), 8)
	buf := f.alloca(fmt.Sprintf("!llvm.array<%d x i8>", tmpBufSize), align)
	capOK := f.op("llvm.icmp \"ule\" %s, %s : i64", c, f.constInt(maxCap, mlir.I64))
	lenOK := f.op("llvm.icmp \"ule\" %s, %s : i64", l, c) // unsigned: also rejects l < 0
	ok := f.op("llvm.and %s, %s : i1", capOK, lenOK)
	yes, no, join := f.newBlock(), f.newBlock(), f.newBlock()
	f.emit("llvm.cond_br %s, %s, %s", ok, yes, no)
	f.b.WriteString("  " + yes + ":\n")
	f.memset(buf, tmpBufSize)
	f.emit("llvm.br %s(%s : !llvm.ptr)", join, buf)
	f.b.WriteString("  " + no + ":\n")
	hp := f.rtcall("makeslice", f.typeDesc(st.Elem()), l, c)
	f.emit("llvm.br %s(%s : !llvm.ptr)", join, hp)
	p := f.newVal()
	f.b.WriteString("  " + join + "(" + p + ": !llvm.ptr):\n")
	f.vals[v] = f.build(tSlice, p, l, c)
	return true
}
