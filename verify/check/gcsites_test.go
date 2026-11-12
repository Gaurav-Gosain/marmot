package main

import (
	"strings"
	"testing"
)

// p.Ptr has its pointer in word 1, p.Num has none. The first site is right; the second
// stores a pointer into a NOSCAN object; the third stores a pointer in word 0, which the
// bitmap does not mark.
func TestSites(t *testing.T) {
	r, err := checkSites("testdata/sites.mlir")
	if err != nil {
		t.Fatal(err)
	}
	if r.checked != 3 || len(r.bad) != 2 {
		t.Fatalf("checked %d, bad %v", r.checked, r.bad)
	}
	if !strings.Contains(r.bad[0]+r.bad[1], "NOSCAN") || !strings.Contains(r.bad[0]+r.bad[1], "word 0 is not a pointer") {
		t.Fatalf("wrong reasons: %v", r.bad)
	}
}

// @good stores a pointer into word 1 of a p.Ptr object and into word 1 of every element of a
// grown []p.Ptr. @bad stores a pointer into a NOSCAN p.Num object, into word 0 of a []p.Ptr
// element (dynamic index, through a block argument) and into a noscan region object.
func TestStores(t *testing.T) {
	r, err := checkStores("testdata/stores.mlir")
	if err != nil {
		t.Fatal(err)
	}
	if r.checked != 5 || len(r.bad) != 3 {
		t.Fatalf("checked %d, bad %v", r.checked, r.bad)
	}
	all := strings.Join(r.bad, "\n")
	for _, w := range []string{"@good"} {
		if strings.Contains(all, w) {
			t.Fatalf("false positive: %v", r.bad)
		}
	}
	for _, w := range []string{"NOSCAN object", "word 0 is not a pointer", "NOSCAN region"} {
		if !strings.Contains(all, w) {
			t.Fatalf("missing %q in %v", w, r.bad)
		}
	}
}
