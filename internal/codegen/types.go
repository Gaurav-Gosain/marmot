package codegen

import (
	"fmt"
	"go/types"

	"github.com/Gaurav-Gosain/marmot/internal/mlir"
)

// Types used for Go values.
var (
	tString = mlir.Struct(mlir.Ptr, mlir.I64)
	tSlice  = mlir.Struct(mlir.Ptr, mlir.I64, mlir.I64)
	tIface  = mlir.Struct(mlir.Ptr, mlir.Ptr)
)

// typeMapper maps Go types to LLVM types. Safe for use by one goroutine.
type typeMapper struct {
	sizes types.Sizes
	val   map[types.Type]string
	mem   map[types.Type]string
}

func newTypeMapper(sizes types.Sizes) *typeMapper {
	return &typeMapper{sizes: sizes, val: map[types.Type]string{}, mem: map[types.Type]string{}}
}

// valType is the LLVM type of an SSA register holding a value of type t.
// It differs from memType only for bool (i1 versus i8) and tuples.
func (m *typeMapper) valType(t types.Type) string {
	if s, ok := m.val[t]; ok {
		return s
	}
	var s string
	switch u := t.Underlying().(type) {
	case *types.Basic:
		if u.Kind() == types.Bool || u.Kind() == types.UntypedBool {
			s = mlir.I1
		} else {
			s = m.memType(t)
		}
	case *types.Tuple:
		if u.Len() == 0 {
			s = mlir.Void
		} else if u.Len() == 1 {
			s = m.valType(u.At(0).Type())
		} else {
			fs := make([]string, u.Len())
			for i := range fs {
				fs[i] = m.valType(u.At(i).Type())
			}
			s = mlir.Struct(fs...)
		}
	case *types.Struct, *types.Array:
		if m.isBig(t) {
			s = mlir.Ptr
		} else {
			s = m.memType(t)
		}
	default:
		s = m.memType(t)
	}
	m.val[t] = s
	return s
}

// bigValueSize is the size above which struct and array values are kept in
// memory: their register form is a pointer to an immutable copy.
const bigValueSize = 256

// isBig reports whether values of t use the indirect register form.
func (m *typeMapper) isBig(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Struct, *types.Array:
		return m.sizes.Sizeof(t) > bigValueSize
	}
	return false
}

// memType is the LLVM type of a value of type t stored in memory.
func (m *typeMapper) memType(t types.Type) string {
	if s, ok := m.mem[t]; ok {
		return s
	}
	var s string
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.Bool, types.UntypedBool:
			s = mlir.I8
		case types.Int8, types.Uint8:
			s = mlir.I8
		case types.Int16, types.Uint16:
			s = mlir.I16
		case types.Int32, types.Uint32, types.UntypedRune:
			s = mlir.I32
		case types.Int, types.Uint, types.Uintptr, types.Int64, types.Uint64, types.UntypedInt:
			s = fmt.Sprintf("i%d", 8*m.sizes.Sizeof(u))
			if u.Kind() == types.UntypedInt {
				s = mlir.I64
			}
		case types.Float32:
			s = mlir.F32
		case types.Float64, types.UntypedFloat:
			s = mlir.F64
		case types.Complex64:
			s = mlir.Struct(mlir.F32, mlir.F32)
		case types.Complex128, types.UntypedComplex:
			s = mlir.Struct(mlir.F64, mlir.F64)
		case types.String, types.UntypedString:
			s = tString
		case types.UnsafePointer, types.UntypedNil:
			s = mlir.Ptr
		case types.Invalid:
			s = mlir.I8 // unused range key or value
		default:
			panic(fmt.Sprintf("memType: unexpected basic type %v", u))
		}
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature:
		s = mlir.Ptr
	case *types.Slice:
		s = tSlice
	case *types.Interface:
		s = tIface
	case *types.Array:
		s = mlir.Array(u.Len(), m.memType(u.Elem()))
	case *types.Struct:
		s = m.structType(u)
	case *types.Tuple:
		s = m.valType(t)
	default:
		panic(fmt.Sprintf("memType: unexpected type %T %v", u, t))
	}
	m.mem[t] = s
	return s
}

// structType builds a literal struct whose natural LLVM layout equals Go's
// layout. Go pads a struct whose last field has size zero; LLVM does not,
// so explicit padding is appended when the sizes differ.
func (m *typeMapper) structType(st *types.Struct) string {
	n := st.NumFields()
	fs := make([]string, 0, n+1)
	vars := make([]*types.Var, n)
	var off, maxAlign int64 = 0, 1
	for i := range n {
		f := st.Field(i)
		vars[i] = f
		fs = append(fs, m.memType(f.Type()))
		a := m.sizes.Alignof(f.Type())
		if a > maxAlign {
			maxAlign = a
		}
		off = align(off, a) + m.sizes.Sizeof(f.Type())
	}
	llvmSize := align(off, maxAlign)
	goSize := m.sizes.Sizeof(st)
	if goSize != llvmSize {
		if goSize < llvmSize {
			panic(fmt.Sprintf("struct layout mismatch for %v: go %d llvm %d", st, goSize, llvmSize))
		}
		// The padding array has alignment 1 and sits right after the last
		// field. goSize is a multiple of the struct alignment, so LLVM adds no
		// further tail padding.
		fs = append(fs, mlir.Array(goSize-off, mlir.I8))
	}
	return mlir.Struct(fs...)
}

// structFieldCount returns the number of LLVM fields of st's memory type
// (the Go fields plus an optional tail padding array).
func (m *typeMapper) structFieldCount(st *types.Struct) int {
	if m.structPadType(st) != "" {
		return st.NumFields() + 1
	}
	return st.NumFields()
}

// structPadType returns the tail padding array type of st, or "".
func (m *typeMapper) structPadType(st *types.Struct) string {
	var off, maxAlign int64 = 0, 1
	for f := range st.Fields() {
		a := m.sizes.Alignof(f.Type())
		if a > maxAlign {
			maxAlign = a
		}
		off = align(off, a) + m.sizes.Sizeof(f.Type())
	}
	if goSize := m.sizes.Sizeof(st); goSize != align(off, maxAlign) {
		return mlir.Array(goSize-off, mlir.I8)
	}
	return ""
}

func align(x, a int64) int64 { return (x + a - 1) / a * a }

// isBool reports whether t is a boolean type (i1 in registers).
func isBool(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsBoolean != 0
}

func basicInfo(t types.Type) types.BasicInfo {
	if b, ok := t.Underlying().(*types.Basic); ok {
		return b.Info()
	}
	return 0
}

func isSigned(t types.Type) bool {
	i := basicInfo(t)
	return i&types.IsInteger != 0 && i&types.IsUnsigned == 0
}

func isInteger(t types.Type) bool { return basicInfo(t)&types.IsInteger != 0 }
func isFloat(t types.Type) bool   { return basicInfo(t)&types.IsFloat != 0 }
func isComplex(t types.Type) bool { return basicInfo(t)&types.IsComplex != 0 }
func isString(t types.Type) bool  { return basicInfo(t)&types.IsString != 0 }

func isUnsafePointer(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.UnsafePointer
}

// isPtrRepr reports whether values of t are a single LLVM ptr.
func isPtrRepr(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature:
		return true
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	}
	return false
}

// isDirectIface reports whether t is stored directly in an interface data
// word (tflagDirectIface in lib/src/runtime/type.go).
func isDirectIface(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Struct:
		return u.NumFields() == 1 && isDirectIface(u.Field(0).Type())
	case *types.Array:
		return u.Len() == 1 && isDirectIface(u.Elem())
	}
	return isPtrRepr(t)
}

func isInterface(t types.Type) bool { return types.IsInterface(t) }

func isEmptyInterface(t types.Type) bool {
	i, ok := t.Underlying().(*types.Interface)
	return ok && i.NumMethods() == 0
}

func intBits(t string) int {
	switch t {
	case mlir.I1:
		return 1
	case mlir.I8:
		return 8
	case mlir.I16:
		return 16
	case mlir.I32:
		return 32
	case mlir.I64:
		return 64
	}
	panic("intBits: " + t)
}
