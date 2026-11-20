package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
