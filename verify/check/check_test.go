package main

import (
	"strings"
	"testing"
)

func run(t *testing.T, before, after string) []Result {
	t.Helper()
	rs, err := CheckFiles("testdata/"+before, "testdata/"+after)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func bad(rs []Result) int {
	n := 0
	for _, r := range rs {
		if r.Rule == "" {
			n++
		}
	}
	return n
}

func TestB3Accepted(t *testing.T) {
	rs := run(t, "b3_before.mlir", "b3_after_ok.mlir")
	if len(rs) != 1 || rs[0].Rule != "B3" {
		t.Fatalf("want one B3 removal, got %v", rs)
	}
}

// Each case removes a check that no rule justifies; the checker must reject it.
func TestRejected(t *testing.T) {
	for _, c := range []struct{ before, after, why string }{
		{"b3_before.mlir", "b3_after_bad.mlir", "index not a counted loop variable"},
		{"sle_before.mlir", "sle_after.mlir", "guard i <= len"},
		{"step2_before.mlir", "step2_after.mlir", "increment by 2"},
		{"nil_before.mlir", "nil_after_bad.mlir", "nil check dominated on one path only"},
	} {
		rs := run(t, c.before, c.after)
		if bad(rs) == 0 {
			t.Errorf("%s: removal accepted: %v", c.why, rs)
		}
	}
}

func TestN2Accepted(t *testing.T) {
	rs := run(t, "nil_before.mlir", "nil_after_ok.mlir")
	if len(rs) != 1 || rs[0].Rule != "N2" {
		t.Fatalf("want one N2 removal, got %v", rs)
	}
}

func TestParseSkipsComments(t *testing.T) {
	fns, err := Parse(strings.NewReader("llvm.func @f() {\n^bb1:  // 2 preds: ^bb0\n  llvm.return\n}\n"))
	if err != nil || len(fns) != 1 || len(fns[0].Blocks) != 2 {
		t.Fatalf("got %v %v", fns, err)
	}
}
