package target

import (
	"go/types"
	"testing"
)

// The wasm layout: gc's integers, 4-byte pointers, and the LLVM types the
// code generator gives strings ({ptr, i64}), slices ({ptr, i64, i64}) and
// interfaces ({ptr, ptr}).
func TestWasmSizes(t *testing.T) {
	s := Sizes("wasm")
	ptr := types.NewPointer(types.Typ[types.Int8])
	str := types.Typ[types.String]
	field := func(name string, t types.Type) *types.Var { return types.NewField(0, nil, name, t, false) }
	st := types.NewStruct([]*types.Var{field("a", types.Typ[types.Uint32]), field("p", ptr), field("n", types.Typ[types.Uintptr]), field("s", str)}, nil)
	for _, c := range []struct {
		name        string
		t           types.Type
		size, align int64
	}{
		{"int", types.Typ[types.Int], 8, 8},
		{"uintptr", types.Typ[types.Uintptr], 8, 8},
		{"pointer", ptr, 4, 4},
		{"unsafe.Pointer", types.Typ[types.UnsafePointer], 4, 4},
		{"map", types.NewMap(str, str), 4, 4},
		{"func", types.NewSignatureType(nil, nil, nil, nil, nil, false), 4, 4},
		{"string", str, 16, 8},
		{"slice", types.NewSlice(ptr), 24, 8},
		{"interface", types.NewInterfaceType(nil, nil), 8, 4},
		{"struct{uint32; *int8; uintptr; string}", st, 32, 8},
		{"[3]*int8", types.NewArray(ptr, 3), 12, 4},
	} {
		if got, al := s.Sizeof(c.t), s.Alignof(c.t); got != c.size || al != c.align {
			t.Errorf("%s: size %d align %d, want %d %d", c.name, got, al, c.size, c.align)
		}
	}
	offs := s.Offsetsof([]*types.Var{st.Field(0), st.Field(1), st.Field(2), st.Field(3)})
	if want := []int64{0, 4, 8, 16}; offs[0] != want[0] || offs[1] != want[1] || offs[2] != want[2] || offs[3] != want[3] {
		t.Errorf("offsets %v, want %v", offs, want)
	}
	if Sizes("arm64").Sizeof(ptr) != 8 || Sizes("").Sizeof(ptr) != 8 {
		t.Error("64-bit pointers are not 8 bytes")
	}
}
