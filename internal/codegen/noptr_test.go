package codegen

import (
	"go/token"
	"go/types"
	"testing"
)

func TestHasPointers(t *testing.T) {
	field := func(name string, t types.Type) *types.Var { return types.NewField(token.NoPos, nil, name, t, false) }
	i64, u8 := types.Typ[types.Int64], types.Typ[types.Uint8]
	cases := []struct {
		t    types.Type
		want bool
	}{
		{i64, false},
		{types.Typ[types.Uintptr], false},
		{types.Typ[types.Float64], false},
		{types.Typ[types.Complex128], false},
		{types.NewArray(u8, 32<<20), false},
		{types.NewArray(types.NewPointer(i64), 0), false},
		{types.NewStruct([]*types.Var{field("a", i64), field("b", types.NewArray(u8, 3))}, nil), false},
		{types.NewStruct(nil, nil), false},
		{types.Typ[types.String], true},
		{types.Typ[types.UnsafePointer], true},
		{types.NewPointer(i64), true},
		{types.NewSlice(u8), true},
		{types.NewMap(i64, i64), true},
		{types.NewChan(types.SendRecv, i64), true},
		{types.NewSignatureType(nil, nil, nil, nil, nil, false), true},
		{types.NewInterfaceType(nil, nil), true},
		{types.NewArray(types.Typ[types.String], 1), true},
		{types.NewStruct([]*types.Var{field("a", i64), field("p", types.NewPointer(u8))}, nil), true},
	}
	for _, c := range cases {
		if got := hasPointers(c.t); got != c.want {
			t.Errorf("hasPointers(%s) = %v, want %v", c.t, got, c.want)
		}
	}
}

// The section names are the contract with the runtime, which skips them
// when it scans the image, and with the driver's ELF linker script.
func TestNoptrSectionName(t *testing.T) {
	cases := []struct {
		goos string
		zero bool
		want string
	}{
		{"darwin", true, "__DATA,__noptrbss,zerofill"},
		{"darwin", false, "__DATA,__noptrdata"},
		{"linux", true, ".bss.marmot.noptr.p.v"},
		{"linux", false, ".data.marmot.noptr.p.v"},
		{"wasip1", true, ""},
		{"wasip1", false, ""},
	}
	for _, c := range cases {
		if got := noptrSectionName(c.goos, "p.v", c.zero); got != c.want {
			t.Errorf("noptrSectionName(%s, zero %v) = %q, want %q", c.goos, c.zero, got, c.want)
		}
	}
}
