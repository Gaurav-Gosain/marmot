package codegen

import (
	"go/types"
	"os"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
	"golang.org/x/tools/go/ssa"
)

// GC pointers for statepoints (MARMOT_GCPTR=1, off by default). Every Go
// pointer value in a register is a pointer in address space 1, derived
// pointers (field and element addresses) included: the backend's statepoint
// lowering finds the GC pointers live across a call by their address
// space and records them in stack maps. Addresses that are not Go values
// stay in address space 0: stack slots (alloca), globals (addressof) and
// the addresses computed from them, so promotion of stack slots to
// registers keeps working. Such an address becomes a GC pointer with an
// addrspacecast where it turns into a Go value (stored, passed, returned,
// compared, merged). Static data keeps plain pointers (mlir.Data).
//
// The frontend adds no gc attribute; the backend picks the strategy.

var optGCPtr = os.Getenv("MARMOT_GCPTR") == "1"

// tP is the register type of a Go pointer value.
var tP = func() string {
	if optGCPtr {
		return mlir.GCPtr
	}
	return mlir.Ptr
}()

// markAddr records that v is an address in address space 0 and returns v.
func (f *funcGen) markAddr(v string) string {
	if trackAddr {
		if f.addr0 == nil {
			f.addr0 = map[string]bool{}
		}
		f.addr0[v] = true
	}
	return v
}

// pty is the pointer type of the address or pointer value v.
func (f *funcGen) pty(v string) string {
	if optGCPtr && !f.addr0[v] {
		return mlir.GCPtr
	}
	return mlir.Ptr
}

// gcv returns v as a Go pointer value: an address in address space 0 is
// cast to a GC pointer. The cast of a stack slot goes in the entry block
// next to the slot, so it dominates every use.
func (f *funcGen) gcv(v string) string {
	if !optGCPtr || !f.addr0[v] {
		return v
	}
	if c, ok := f.slotCast[v]; ok {
		return c
	}
	c := f.newVal()
	if f.slot0[v] {
		if f.slotCast == nil {
			f.slotCast = map[string]string{}
		}
		f.slotCast[v] = c
		f.pro.WriteString("    " + c + " = llvm.addrspacecast " + v + " : !llvm.ptr to !llvm.ptr<1>\n")
		return c
	}
	f.b.WriteString("    " + c + " = llvm.addrspacecast " + v + " : !llvm.ptr to !llvm.ptr<1>" + f.opLoc() + "\n")
	return c
}

// derived records the address computed from base: in address space 0 when
// base is. It returns v.
func (f *funcGen) derived(base, v string) string {
	if optWB && f.fresh[base] {
		f.fresh[v] = true
	}
	if trackAddr && f.addr0[base] {
		f.addr0[v] = true
		if f.stack0[base] {
			f.stack0[v] = true
		}
	}
	return v
}

// gcArgs returns the call arguments args as Go values.
func (f *funcGen) gcArgs(args []string) []string {
	if !optGCPtr {
		return args
	}
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = f.gcv(a)
	}
	return out
}

// asVal returns v, a register value of LLVM type vt, as a Go value: cast
// when vt is the GC pointer type.
func (f *funcGen) asVal(v, vt string) string {
	if vt == tP {
		return f.gcv(v)
	}
	return v
}

// conservativeFunc reports whether fn's frame must be scanned
// conservatively even when the backend makes stack maps (MARMOT_GCPTR=1):
// it may hold heap pointers the address spaces do not show. Its function
// gets the attribute "marmot-conservative-stack". That is: a linknamed
// function (it follows another package's conventions), a conversion
// between unsafe.Pointer and uintptr, unsafe.Add, Slice, String,
// SliceData or StringData, or a parameter, result or value whose type is a
// struct holding an unsafe.Pointer (reflect.Value style).
func (p *Program) conservativeFunc(fn *ssa.Function) bool {
	if _, ok := p.linkname[fn]; ok {
		return true
	}
	for _, v := range fn.Params {
		if unsafeAggregate(v.Type()) {
			return true
		}
	}
	if r := fn.Signature.Results(); r != nil {
		for i := 0; i < r.Len(); i++ {
			if unsafeAggregate(r.At(i).Type()) {
				return true
			}
		}
	}
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch x := instr.(type) {
			case *ssa.Convert:
				if isUnsafePointer(x.Type()) != isUnsafePointer(x.X.Type()) &&
					(isUintptr(x.Type()) || isUintptr(x.X.Type())) {
					return true
				}
			case ssa.CallInstruction:
				if bi, ok := x.Common().Value.(*ssa.Builtin); ok {
					switch bi.Name() {
					case "Add", "Slice", "String", "SliceData", "StringData":
						return true
					}
				}
			}
			if v, ok := instr.(ssa.Value); ok && unsafeAggregate(v.Type()) {
				return true
			}
		}
	}
	return false
}

// unsafeAggregate reports whether t is a struct with an unsafe.Pointer
// field (at any depth).
func unsafeAggregate(t types.Type) bool {
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		ft := st.Field(i).Type()
		if isUnsafePointer(ft) || unsafeAggregate(ft) {
			return true
		}
	}
	return false
}

func isUintptr(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uintptr
}
