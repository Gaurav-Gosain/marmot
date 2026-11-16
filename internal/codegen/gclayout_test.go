package codegen

import (
	"bytes"
	"go/token"
	"go/types"
	"testing"

	"github.com/Gaurav-Gosain/marmot/internal/target"
)

// gcdata has one bit per pointer-sized word: 4 bytes on wasm, 8 elsewhere.
// A pointer at offset 4 on wasm is word 1. With 8-byte words it fell into
// bit 0 with the pointer before it, and the collector never scanned it
// (the captured variable of a closure, the second word of an interface).
func TestGCLayout(t *testing.T) {
	field := func(name string, t types.Type) *types.Var { return types.NewField(token.NoPos, nil, name, t, false) }
	ptr := types.NewPointer(types.Typ[types.Int])
	up := types.Typ[types.UnsafePointer]
	i32, i64 := types.Typ[types.Int32], types.Typ[types.Int64]
	str := types.Typ[types.String]
	closure := types.NewStruct([]*types.Var{field("F", up), field("X0", ptr)}, nil)
	iface := types.NewInterfaceType(nil, nil)
	after32 := types.NewStruct([]*types.Var{field("a", i32), field("p", ptr)}, nil)
	strAfter32 := types.NewStruct([]*types.Var{field("a", i32), field("s", str)}, nil)
	mixed := types.NewStruct([]*types.Var{field("n", i64), field("e", iface), field("p", ptr), field("b", i32), field("q", ptr)}, nil)
	cases := []struct {
		name    string
		arch    string
		t       types.Type
		size    int64
		ptrdata int64
		bm      []byte
	}{
		{"closure", "wasm", closure, 8, 8, []byte{0x03}},
		{"closure", "amd64", closure, 16, 16, []byte{0x03}},
		{"interface", "wasm", iface, 8, 8, []byte{0x03}},
		{"interface", "amd64", iface, 16, 16, []byte{0x03}},
		{"int32 then pointer", "wasm", after32, 8, 8, []byte{0x02}},
		{"int32 then pointer", "amd64", after32, 16, 16, []byte{0x02}},
		// string {pointer, int64}: 8-aligned, so at offset 8 (word 2 on wasm)
		{"int32 then string", "wasm", strAfter32, 24, 12, []byte{0x04}},
		{"int32 then string", "amd64", strAfter32, 24, 16, []byte{0x02}},
		// wasm: n at 0, e at 8 (words 2, 3), p at 16 (word 4), b at 20, q at 24
		// (word 6); size 28 rounded up to the 8-byte alignment
		{"mixed", "wasm", mixed, 32, 28, []byte{0x5c}},
		{"mixed", "amd64", mixed, 48, 48, []byte{0x2e}},
		{"int64", "wasm", i64, 8, 0, nil},
	}
	for _, c := range cases {
		sizes := target.Sizes(c.arch)
		if got := sizes.Sizeof(c.t); got != c.size {
			t.Errorf("%s on %s: size %d, want %d", c.name, c.arch, got, c.size)
		}
		ptrdata, bm := gcLayout(c.t, sizes)
		if ptrdata != c.ptrdata || !bytes.Equal(bm, c.bm) {
			t.Errorf("%s on %s: ptrdata %d gcdata %x, want %d %x", c.name, c.arch, ptrdata, bm, c.ptrdata, c.bm)
		}
	}
}
