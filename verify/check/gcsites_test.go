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
