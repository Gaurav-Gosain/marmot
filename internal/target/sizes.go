// Package target holds what marmot lays out per target architecture.
package target

import "go/types"

// Sizes returns the type sizes marmot lays out values with for goarch: gc's
// for every 64-bit target, and for wasm (wasip1, 32-bit memory) gc's wasm
// integers (int, uint and uintptr are 8 bytes) with 4-byte pointers: a
// pointer, map, chan, func or unsafe.Pointer is 4 bytes, a string is
// {pointer, int64} (16 bytes), a slice {pointer, int64, int64} (24) and an
// interface {pointer, pointer} (8). Every part of the compiler uses this
// one function, so unsafe.Sizeof, the descriptors and the generated code
// agree.
func Sizes(goarch string) types.Sizes {
	if goarch == "wasm" {
		return wasmSizes{}
	}
	if s := types.SizesFor("gc", goarch); s != nil {
		return s
	}
	return types.SizesFor("gc", "amd64") // no target given (tests)
}

// PtrSize is the size of a pointer on goarch.
func PtrSize(goarch string) int64 {
	if goarch == "wasm" {
		return 4
	}
	return 8
}

// wasmSizes is gc's layout algorithm (go/types gcsizes.go) with 8-byte
// integers and 4-byte pointers.
type wasmSizes struct{}

const (
	wasmPtr      = 4
	wasmWord     = 8 // int, uint, uintptr, a length
	wasmMaxAlign = 8
)

var basicSizes = [...]int64{
	types.Bool:       1,
	types.Int8:       1,
	types.Int16:      2,
	types.Int32:      4,
	types.Int64:      8,
	types.Uint8:      1,
	types.Uint16:     2,
	types.Uint32:     4,
	types.Uint64:     8,
	types.Float32:    4,
	types.Float64:    8,
	types.Complex64:  8,
	types.Complex128: 16,
}

func (s wasmSizes) Alignof(T types.Type) int64 {
	switch t := T.Underlying().(type) {
	case *types.Array:
		return s.Alignof(t.Elem())
	case *types.Struct:
		if t.NumFields() == 0 && isSyncAtomicAlign64(T) {
			return 8
		}
		max := int64(1)
		for i := range t.NumFields() {
			if a := s.Alignof(t.Field(i).Type()); a > max {
				max = a
			}
		}
		return max
	case *types.Slice, *types.Basic:
		if b, ok := t.(*types.Basic); ok && b.Info()&types.IsString == 0 {
			break
		}
		return wasmWord // {pointer, int64...}: aligned like the int64
	case *types.Interface:
		return wasmPtr
	}
	a := s.Sizeof(T)
	if a < 1 {
		return 1
	}
	if b, ok := T.Underlying().(*types.Basic); ok && b.Info()&types.IsComplex != 0 {
		a /= 2
	}
	return min(a, wasmMaxAlign)
}

func (s wasmSizes) Offsetsof(fields []*types.Var) []int64 {
	offsets := make([]int64, len(fields))
	var offs int64
	for i, f := range fields {
		if offs < 0 {
			offsets[i] = -1
			continue
		}
		offs = align(offs, s.Alignof(f.Type()))
		offsets[i] = offs
		if d := s.Sizeof(f.Type()); d >= 0 && offs >= 0 {
			offs += d
		} else {
			offs = -1
		}
	}
	return offsets
}

func (s wasmSizes) Sizeof(T types.Type) int64 {
	switch t := T.Underlying().(type) {
	case *types.Basic:
		k := t.Kind()
		if int(k) < len(basicSizes) && basicSizes[k] > 0 {
			return basicSizes[k]
		}
		switch k {
		case types.String:
			return 16
		case types.Int, types.Uint, types.Uintptr:
			return wasmWord
		}
		return wasmPtr // unsafe.Pointer
	case *types.Array:
		n := t.Len()
		if n <= 0 {
			return 0
		}
		esize := s.Sizeof(t.Elem())
		if esize < 0 {
			return -1
		}
		if esize == 0 {
			return 0
		}
		const maxInt64 = 1<<63 - 1
		if esize > maxInt64/n {
			return -1
		}
		return esize * n
	case *types.Slice:
		return 24
	case *types.Struct:
		n := t.NumFields()
		if n == 0 {
			return 0
		}
		fields := make([]*types.Var, n)
		for i := range fields {
			fields[i] = t.Field(i)
		}
		offsets := s.Offsetsof(fields)
		offs := offsets[n-1]
		size := s.Sizeof(fields[n-1].Type())
		if offs < 0 || size < 0 {
			return -1
		}
		if offs > 0 && size == 0 {
			size = 1 // gc: the last field of a non-zero-sized struct has size > 0
		}
		return align(offs+size, s.Alignof(t))
	case *types.Interface:
		return 2 * wasmPtr
	}
	return wasmPtr // pointer, map, chan, func
}

func align(x, a int64) int64 {
	y := x + a - 1
	return y - y%a
}

// isSyncAtomicAlign64 reports whether T is sync/atomic.align64 (an empty
// struct that makes its container 8-byte aligned).
func isSyncAtomicAlign64(T types.Type) bool {
	n, ok := T.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	return obj.Name() == "align64" && obj.Pkg() != nil &&
		(obj.Pkg().Path() == "sync/atomic" || obj.Pkg().Path() == "internal/runtime/atomic")
}
