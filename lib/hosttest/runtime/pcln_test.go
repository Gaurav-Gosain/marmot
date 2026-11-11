package runtime

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

// TestPclnLookup compares lookups in a pc table taken from a marmot built
// fmt hello (version 2) with the backend's reference decoder
// (backend/test/driver/Inputs/pcln_decode.py), whose output is in
// testdata/fmt_hello.want as "offset function file:line" lines.
func TestPclnLookup(t *testing.T) {
	data, err := os.ReadFile("testdata/fmt_hello.pcln")
	if err != nil {
		t.Fatal(err)
	}
	const base = 0x100000
	var tab pclnTable
	if !tab.init(unsafe.Pointer(&data[0]), uintptr(len(data)), base) {
		t.Fatal("init failed")
	}
	f, err := os.Open("testdata/fmt_hello.want")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		off, _ := strconv.ParseUint(fs[0], 0, 64)
		info, ok := tab.lookup(base + uintptr(off))
		if !ok {
			t.Fatalf("%s: no function", fs[0])
		}
		got := info.namePrefix + info.name + " " + info.filePrefix + info.file + ":" + strconv.Itoa(info.line)
		if want := fs[1] + " " + fs[2]; got != want {
			t.Errorf("%s: got %s, want %s", fs[0], got, want)
		}
		n++
	}
	if n < 100 {
		t.Fatalf("only %d lookups", n)
	}
	if _, ok := tab.lookup(base - 1); ok {
		t.Error("lookup below the base")
	}
	if _, ok := tab.lookup(base + uintptr(tab.textEnd)); ok {
		t.Error("lookup past the text end")
	}
	var bad pclnTable
	if bad.init(unsafe.Pointer(&data[1]), uintptr(len(data)-1), 0) {
		t.Error("init accepted a table without the magic")
	}
}
