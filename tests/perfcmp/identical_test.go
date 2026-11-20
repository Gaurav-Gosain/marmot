package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSameBinary(t *testing.T) {
	dir := t.TempDir()
	w := func(name, s string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ra, rb, rc := "/t/trees/aaaa1", "/t/trees/bbbb2", "/t/trees/cc3"
	a := w("a", "\x7fELF "+ra+"/lib/src/fmt/print.go x "+ra+"/bench/p.go")
	b := w("b", "\x7fELF "+rb+"/lib/src/fmt/print.go x "+rb+"/bench/p.go")
	c := w("c", "\x7fELF "+rb+"/lib/src/fmt/print.go y "+rb+"/bench/p.go")
	d := w("d", "\x7fELF "+rc+"/lib/src/fmt/print.go x "+rc+"/bench/p.go")
	for _, tc := range []struct {
		name         string
		x, y, rx, ry string
		want         bool
	}{
		{"roots differ, rest same", a, b, ra, rb, true},
		{"roots differ, other bytes too", a, c, ra, rb, false},
		{"same root, files differ", a, b, ra, ra, false},
		{"roots of other lengths", a, d, ra, rc, false},
		{"byte-identical", a, a, ra, rb, true},
	} {
		if got := sameBinary(tc.x, tc.y, tc.rx, tc.ry); got != tc.want {
			t.Errorf("%s: sameBinary = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	w := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	big := make([]byte, 3<<20+5) // more than one read buffer, not a multiple of it
	for i := range big {
		big[i] = byte(i * 7)
	}
	other := append([]byte(nil), big...)
	other[len(other)-1]++
	a, b, c := w("a", big), w("b", big), w("c", other)
	d := w("d", big[:len(big)-1])
	e, f := w("e", nil), w("f", nil)
	for _, tc := range []struct {
		x, y string
		want bool
	}{{a, b, true}, {a, c, false}, {a, d, false}, {e, f, true}, {a, filepath.Join(dir, "missing"), false}} {
		if got := sameFile(tc.x, tc.y); got != tc.want {
			t.Errorf("sameFile(%s, %s) = %v, want %v", filepath.Base(tc.x), filepath.Base(tc.y), got, tc.want)
		}
	}
}
