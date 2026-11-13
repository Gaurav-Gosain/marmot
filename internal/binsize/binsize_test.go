package binsize

import (
	"encoding/binary"
	"testing"
)

func TestPackageOf(t *testing.T) {
	for name, want := range map[string]string{
		"main.main":                    "main",
		"fmt.(*pp).printValue":         "fmt",
		"internal/strconv.fmtEFG":      "internal/strconv",
		"example.com/a/b.T.M":          "example.com/a/b",
		"rt_malloc":                    "C",
		"time.Time.appendFormat":       "time",
		"github.com/x/y.(*Z[...]).Go":  "github.com/x/y",
		"(context.cancelCtx).Deadline": "context",
		"(*math/big.byteReader).Token": "math/big",
		"(*internal/sync.entry[any, any]).entry@internal/sync$tfn":     "internal/sync",
		"(*internal/reflectlite.rtype).common@internal/reflectlite$fv": "internal/reflectlite",
		"reflectcall:func(int)(bool)":                                  "reflect",
		"open$fv":                                                      "C",
	} {
		if got := PackageOf(name); got != want {
			t.Errorf("PackageOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// A version 2 table with two functions: "main.main" at 0x100 and the C
// function "rt_x" at 0x180, text ending at 0x200.
func TestReadPcln(t *testing.T) {
	const hdr = 8 + 13*4
	strs := []byte{0}                   // offset 0: empty
	pkgStr := len(strs)                 // "main."
	strs = append(strs, "main.\x00"...) //
	nameMain := len(strs)               // uvarint(1) + "main"
	strs = append(strs, 1)              //
	strs = append(strs, "main\x00"...)  //
	nameX := len(strs)                  // uvarint(0) + "rt_x"
	strs = append(strs, 0)              //
	strs = append(strs, "rt_x\x00"...)  //
	funcOff := hdr
	pkgOff := funcOff + 24
	strOff := pkgOff + 4
	t32 := make([]byte, strOff)
	copy(t32, "MRMTPCLN")
	put := func(off, v int) { binary.LittleEndian.PutUint32(t32[off:], uint32(v)) }
	put(8, 2)      // version
	put(12, 2)     // nfuncs
	put(24, 1)     // npkgs
	put(28, 4)     // quantum
	put(32, 0x200) // textEnd
	put(36, funcOff)
	put(48, pkgOff)
	put(56, strOff)
	put(funcOff, 0x100)
	put(funcOff+4, nameMain)
	put(funcOff+12, 0x180)
	put(funcOff+16, nameX)
	put(pkgOff, pkgStr)
	table := append(t32, strs...)
	var b Binary
	if err := b.readPcln(table); err != nil {
		t.Fatal(err)
	}
	if len(b.Funcs) != 2 || b.Funcs[0].Name != "main.main" || b.Funcs[0].Size != 0x80 || b.Funcs[1].Name != "rt_x" || b.Funcs[1].Pkg != "C" || b.Funcs[1].Size != 0x80 {
		t.Errorf("funcs %+v", b.Funcs)
	}
}

// A C origin table at the end of a pc table section names the C functions
// it covers; the others stay "C".
func TestCOrigins(t *testing.T) {
	var d []byte
	le := binary.LittleEndian
	d = le.AppendUint32(d, 2)
	for _, l := range []string{"runtime core (C)", "example.com/p (C)"} {
		d = le.AppendUint16(d, uint16(len(l)))
		d = append(d, l...)
	}
	d = le.AppendUint32(d, 2)
	for _, r := range [][3]uint64{{0x100, 0x200, 1}, {0x300, 0x380, 0}} {
		d = le.AppendUint64(d, r[0])
		d = le.AppendUint64(d, r[1])
		d = le.AppendUint16(d, uint16(r[2]))
	}
	t2 := append([]byte("pc table bytes"), d...)
	t2 = le.AppendUint32(t2, uint32(len(d)))
	t2 = append(t2, "MCOR"...)
	b := &Binary{Funcs: []Func{{Name: "a", Pkg: "C", Start: 0x100}, {Name: "b", Pkg: "C", Start: 0x1f0}, {Name: "c", Pkg: "C", Start: 0x200},
		{Name: "d", Pkg: "C", Start: 0x340}, {Name: "main.main", Pkg: "main", Start: 0x150}}}
	b.applyCOrigins(t2)
	want := []string{"example.com/p (C)", "example.com/p (C)", "C", "runtime core (C)", "main"}
	for i, f := range b.Funcs {
		if f.Pkg != want[i] {
			t.Errorf("%s: %q, want %q", f.Name, f.Pkg, want[i])
		}
	}
	if !b.COrigins {
		t.Error("COrigins not set")
	}
	b2 := &Binary{Funcs: []Func{{Name: "a", Pkg: "C", Start: 0x100}}}
	b2.applyCOrigins([]byte("no table"))
	if b2.Funcs[0].Pkg != "C" || b2.COrigins {
		t.Error("a section without a table changed the functions")
	}
}
