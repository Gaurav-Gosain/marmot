package main

import (
	"go/token"
	"go/types"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func field(name string, t types.Type) *types.Var {
	return types.NewField(token.NoPos, nil, name, t, false)
}

func structOf(fs ...*types.Var) *types.Struct { return types.NewStruct(fs, nil) }

// TestLayoutWordSize checks the layout model at both pointer sizes: one bitmap slot per
// pointer, and on wasm pointers at offset 4 mod 8.
func TestLayoutWordSize(t *testing.T) {
	ptr := types.NewPointer(types.Typ[types.Int])
	i32 := types.Typ[types.Int32]
	empty := types.NewInterfaceType(nil, nil)
	cases := []struct {
		name string
		t    types.Type
		// size and pointer slots per target
		size64, sizeWasm int64
		ptr64, ptrWasm   []int
	}{
		{"int32 then pointer", structOf(field("a", i32), field("p", ptr)), 16, 8, []int{1}, []int{1}},
		{"interface", empty, 16, 8, []int{0, 1}, []int{0, 1}},
		{"interface, byte, pointer", structOf(field("i", empty), field("b", types.Typ[types.Byte]), field("p", ptr)), 32, 16, []int{0, 1, 3}, []int{0, 1, 3}},
		{"string then pointer", structOf(field("s", types.Typ[types.String]), field("p", ptr)), 24, 24, []int{0, 2}, []int{0, 4}},
		{"slice", types.NewSlice(ptr), 24, 24, []int{0}, []int{0}},
		{"int then pointer", structOf(field("n", types.Typ[types.Int]), field("p", ptr)), 16, 16, []int{1}, []int{2}},
		{"array of pointers", types.NewArray(ptr, 3), 24, 12, []int{0, 1, 2}, []int{0, 1, 2}},
		{"no pointers", structOf(field("a", i32), field("n", types.Typ[types.Int64])), 16, 16, nil, nil},
	}
	for _, goarch := range []string{"arm64", "wasm"} {
		m, err := modelFor(goarch)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			l, err := m.layoutOf(c.t, 0)
			if err != nil {
				t.Fatalf("%s %s: %v", goarch, c.name, err)
			}
			size, want := c.size64, c.ptr64
			if goarch == "wasm" {
				size, want = c.sizeWasm, c.ptrWasm
			}
			var got []int
			for w, p := range l.ptr {
				if p {
					got = append(got, w)
				}
			}
			if l.size != size || !slices.Equal(got, want) {
				t.Errorf("%s %s: size %d, pointer slots %v; want %d, %v", goarch, c.name, l.size, got, size, want)
			}
		}
	}
}

// TestReadDescsGoInit reads descriptors and bitmaps in the one-line go.init form.
func TestReadDescsGoInit(t *testing.T) {
	mlir := `  llvm.mlir.global linkonce_odr constant @"type:p.pair.gcdata"() {addr_space = 0 : i32, go.init = "(i2;)"} : !llvm.array<1 x i8>
  llvm.mlir.global weak_odr constant @"type:p.pair"() {addr_space = 0 : i32, go.init = "((i16;i16;a18:type:p.pair.gcdata(a9:go:str.abi9;)a20:type:p.pair.uncommoni25;i8;i8;i3;)(a9:go:str.abi4;)a18:type:p.pair.fieldsi2;)"} : !llvm.struct<(!llvm.struct<(i64, i64, ptr, !llvm.struct<(ptr, i64)>, ptr, i8, i8, i8, i8)>, !llvm.struct<(ptr, i64)>, ptr, i64)>
  llvm.mlir.global linkonce_odr constant @"type:int"() {addr_space = 0 : i32, go.init = "(i8;i0;z(a9:go:str.abi3;)a17:type:int.uncommoni2;i8;i8;i7;)"} : !llvm.struct<(i64, i64, ptr, !llvm.struct<(ptr, i64)>, ptr, i8, i8, i8, i8)>
  llvm.mlir.global linkonce_odr constant @"type:p.pair.fields"() {addr_space = 0 : i32, go.init = "(i1;)"} : !llvm.array<1 x i8>
`
	path := t.TempDir() + "/m.mlir"
	if err := os.WriteFile(path, []byte(mlir), 0o644); err != nil {
		t.Fatal(err)
	}
	ds, err := readDescs(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 {
		t.Fatalf("%d descriptors, want 2 (p.pair, int): %v", len(ds), ds)
	}
	p := ds["p.pair"]
	if p == nil || p.size != 16 || p.ptrdata != 16 || !p.hasBits || len(p.bits) != 1 || p.bits[0] != 2 {
		t.Errorf("p.pair: %+v, want size 16, ptrdata 16, bitmap [2]", p)
	}
	if i := ds["int"]; i == nil || i.size != 8 || i.ptrdata != 0 || i.hasBits {
		t.Errorf("int: %+v, want size 8, ptrdata 0, no bitmap", i)
	}
}

// TestReadDescsUndecoded checks that a descriptor global readDescs cannot decode is an
// error: an empty check must not pass.
func TestReadDescsUndecoded(t *testing.T) {
	good := `  llvm.mlir.global linkonce_odr constant @"type:int"() {addr_space = 0 : i32, go.init = "(i8;i0;z(a9:go:str.abi3;)zi2;i8;i8;i7;)"} : !llvm.struct<(i64, i64, ptr, !llvm.struct<(ptr, i64)>, ptr, i8, i8, i8, i8)>
`
	write := func(s string) string {
		t.Helper()
		p := t.TempDir() + "/m.mlir"
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := readDescs(write(good)); err != nil {
		t.Fatalf("good form: %v", err)
	}
	// a form neither decoder knows
	other := strings.Replace(good, "go.init", "go.data", 1)
	if _, err := readDescs(write(other)); err == nil || !strings.Contains(err.Error(), "not decoded") {
		t.Errorf("unknown form: err %v, want a not decoded error", err)
	}
	// the decoder broken on purpose: it matches nothing
	saved := reGoInit
	reGoInit = regexp.MustCompile(`$^`)
	defer func() { reGoInit = saved }()
	if _, err := readDescs(write(good)); err == nil {
		t.Error("a decoder that reads nothing passed")
	}
}

// TestReadDescsSuffixNames checks types whose names end like a side table: main.data and
// main.fields are descriptors (there is no type:main), p.pair.fields is p.pair's side table.
func TestReadDescsSuffixNames(t *testing.T) {
	desc := func(name string) string {
		return "  llvm.mlir.global linkonce_odr constant @\"type:" + name + "\"() {addr_space = 0 : i32, go.init = \"(i8;i0;z(a9:go:str.abi3;)zi2;i8;i8;i7;)\"} : !llvm.struct<(i64, i64, ptr, !llvm.struct<(ptr, i64)>, ptr, i8, i8, i8, i8)>\n"
	}
	side := "  llvm.mlir.global linkonce_odr constant @\"type:p.pair.fields\"() {addr_space = 0 : i32, go.init = \"(i1;)\"} : !llvm.array<1 x i8>\n"
	path := t.TempDir() + "/m.mlir"
	if err := os.WriteFile(path, []byte(desc("main.data")+desc("main.fields")+desc("main.e")+desc("p.pair")+side), 0o644); err != nil {
		t.Fatal(err)
	}
	ds, err := readDescs(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"main.data", "main.fields", "main.e", "p.pair"} {
		if ds[n] == nil {
			t.Errorf("type:%s not read as a descriptor", n)
		}
	}
	if ds["p.pair.fields"] != nil || len(ds) != 4 {
		t.Errorf("descriptors %v, want main.data, main.fields, main.e and p.pair", ds)
	}
}

// TestCompareWasmBitmap checks that a wasm descriptor whose bitmap has one bit per 8 bytes
// (the pointer at offset 4 left unmarked) is a mismatch, and the right one is not.
func TestCompareWasmBitmap(t *testing.T) {
	m, err := modelFor("wasm")
	if err != nil {
		t.Fatal(err)
	}
	st := structOf(field("a", types.Typ[types.Int32]), field("p", types.NewPointer(types.Typ[types.Int])))
	l, err := m.layoutOf(st, 0)
	if err != nil {
		t.Fatal(err)
	}
	good := &tdesc{size: 8, ptrdata: 8, hasBits: true, bits: []byte{0b10}}
	if p := m.compare(good, l); len(p) != 0 {
		t.Errorf("correct descriptor: %v", p)
	}
	// 8-byte words: offset 4 is in word 0, and ptrdata covers the whole first word.
	wrong := &tdesc{size: 8, ptrdata: 8, hasBits: true, bits: []byte{0b01}}
	if p := m.compare(wrong, l); len(p) == 0 {
		t.Error("an 8-byte word bitmap on wasm passed")
	}
}
