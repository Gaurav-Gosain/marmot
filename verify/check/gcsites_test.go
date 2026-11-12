package main

import (
	"strings"
	"testing"
)

// p.Ptr has its pointer in word 1, p.Num has none. The first site is right; the second
// stores a pointer into a NOSCAN object; the third stores a pointer in word 0, which the
// bitmap does not mark; the map update stores a key holding a pointer into a map whose key
// descriptor is p.Num (its element is right).
func TestSites(t *testing.T) {
	r, err := checkSites("testdata/sites.mlir")
	if err != nil {
		t.Fatal(err)
	}
	if r.checked != 5 || len(r.bad) != 3 {
		t.Fatalf("checked %d, bad %v", r.checked, r.bad)
	}
	if !strings.Contains(r.bad[0]+r.bad[1], "NOSCAN") || !strings.Contains(r.bad[0]+r.bad[1], "word 0 is not a pointer") ||
		!strings.Contains(r.bad[2], "map key") {
		t.Fatalf("wrong reasons: %v", r.bad)
	}
}

// @good stores a pointer into word 1 of a p.Ptr object and into word 1 of every element of a
// grown []p.Ptr. @bad stores a pointer into a NOSCAN p.Num object, into word 0 of a []p.Ptr
// element (dynamic index, through a block argument) and into a noscan region object, and
// sends a pointer on a channel whose element descriptor is p.Num.
func TestStores(t *testing.T) {
	r, err := checkStores("testdata/stores.mlir")
	if err != nil {
		t.Fatal(err)
	}
	if r.checked != 7 || len(r.bad) != 4 {
		t.Fatalf("checked %d, bad %v", r.checked, r.bad)
	}
	all := strings.Join(r.bad, "\n")
	for _, w := range []string{"@good"} {
		if strings.Contains(all, w) {
			t.Fatalf("false positive: %v", r.bad)
		}
	}
	for _, w := range []string{"NOSCAN object", "word 0 is not a pointer", "NOSCAN region", "go.make_chan"} {
		if !strings.Contains(all, w) {
			t.Fatalf("missing %q in %v", w, r.bad)
		}
	}
}
