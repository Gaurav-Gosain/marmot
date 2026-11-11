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

// TestPclnInlineFrames compares the logical frames of a version 3 table
// (inline trees) with the reference decoder's frames(): each line of
// testdata/fmt_hello_v3.want is an offset and its frames, innermost first,
// tab separated.
func TestPclnInlineFrames(t *testing.T) {
	data, err := os.ReadFile("testdata/fmt_hello_v3.pcln")
	if err != nil {
		t.Fatal(err)
	}
	const base = 0x100000
	var tab pclnTable
	if !tab.init(unsafe.Pointer(&data[0]), uintptr(len(data)), base) || tab.ninl == 0 {
		t.Fatal("init failed or no inline trees")
	}
	want, err := os.ReadFile("testdata/fmt_hello_v3.want")
	if err != nil {
		t.Fatal(err)
	}
	deep := 0
	for _, line := range strings.Split(strings.TrimSpace(string(want)), "\n") {
		fs := strings.Split(line, "\t")
		off, _ := strconv.ParseUint(fs[0], 0, 64)
		var out [32]pcInfo
		n := tab.frames(base+uintptr(off), out[:])
		var got []string
		for _, f := range out[:n] {
			s := f.namePrefix + f.name
			if f.line != 0 || f.file != "" {
				s += " " + f.filePrefix + f.file + ":" + strconv.Itoa(f.line)
			}
			got = append(got, s)
		}
		if g, w := strings.Join(got, "\t"), strings.Join(fs[1:], "\t"); g != w {
			t.Errorf("%s:\n got  %s\n want %s", fs[0], g, w)
		}
		if n > 2 {
			deep++
		}
	}
	if deep == 0 {
		t.Error("no nested inline frames in the test data")
	}
	// A short output buffer truncates instead of overrunning.
	var one [1]pcInfo
	for _, line := range strings.Split(string(want), "\n")[:5] {
		off, _ := strconv.ParseUint(strings.Split(line, "\t")[0], 0, 64)
		if tab.frames(base+uintptr(off), one[:]) != 1 {
			t.Error("frames with a one entry buffer")
		}
	}
}
