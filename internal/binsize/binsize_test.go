package binsize

import (
	"encoding/binary"
	"fmt"
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

// Lines decodes a function's line program: rows where the line or the
// file changes, files named with their directory prefix.
func TestLines(t *testing.T) {
	const hdr = 8 + 13*4
	strs := []byte{0}
	dir := len(strs)
	strs = append(strs, "/w/\x00"...)
	fileA := len(strs)
	strs = append(strs, 1)
	strs = append(strs, "a.go\x00"...)
	fileB := len(strs)
	strs = append(strs, 1)
	strs = append(strs, "b.go\x00"...)
	name := len(strs)
	strs = append(strs, 0)
	strs = append(strs, "main.F\x00"...)
	// n=2, entry line 10 in file 0; +8 bytes: line 13; +4: line 10 of file 1.
	prog := []byte{2, 10, 0, 2, 12, 1, 9, 1}
	funcOff := hdr
	fileOff := funcOff + 12
	dirOff := fileOff + 8
	lineOff := dirOff + 4
	strOff := lineOff + len(prog)
	t32 := make([]byte, lineOff)
	copy(t32, "MRMTPCLN")
	put := func(off, v int) { binary.LittleEndian.PutUint32(t32[off:], uint32(v)) }
	put(8, 2)      // version
	put(12, 1)     // nfuncs
	put(16, 2)     // nfiles
	put(20, 1)     // ndirs
	put(28, 4)     // quantum
	put(32, 0x200) // textEnd
	put(36, funcOff)
	put(40, fileOff)
	put(44, dirOff)
	put(52, lineOff)
	put(56, strOff)
	put(funcOff, 0x100)
	put(funcOff+4, name)
	put(funcOff+8, 0)
	put(fileOff, fileA)
	put(fileOff+4, fileB)
	put(dirOff, dir)
	table := append(append(t32, prog...), strs...)
	var b Binary
	if err := b.readPcln(table); err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(b.Lines(b.Funcs[0]))
	if want := "[{256 /w/a.go 10} {264 /w/a.go 13} {268 /w/b.go 10}]"; got != want {
		t.Errorf("rows %s, want %s", got, want)
	}
}
