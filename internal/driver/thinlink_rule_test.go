package driver

import (
	"strings"
	"testing"
)

// A type with two methods; main calls Read through an interface, never
// Write: Write's code cell is null and its function stays dead.
func TestMethodRule(t *testing.T) {
	g := newRefGraph()
	g.roots = []string{"main"}
	g.refs["main"] = []string{"type:T"}
	g.refs["type:T"] = []string{"type:T.methods"}
	g.refs["T.Read$ifn"] = []string{"helper"}
	sc := &tlSidecar{
		Arrays: []tlArray{{Sym: "type:T.methods", Elem: []string{"name", "mtyp", "ifn"}, Code: []int{2},
			Rows: []tlRow{
				{Key: [2]string{"Read", "s1"}, Cells: []string{"go:str.Read", "type:func()", "T.Read$ifn"}},
				{Key: [2]string{"Write", "s2"}, Cells: []string{"go:str.Write", "type:func()", "T.Write$ifn"}},
			}}},
		Calls: map[string][][2]string{"main": {{"Read", "s1"}}},
	}
	live, rows := methodLive(g, []*tlSidecar{sc})
	if !live["T.Read$ifn"] || !live["helper"] || live["T.Write$ifn"] || !live["go:str.Write"] {
		t.Fatalf("live: %v", live)
	}
	if !rows["type:T.methods"][0] || rows["type:T.methods"][1] {
		t.Fatalf("rows: %v", rows)
	}
	ir := methodArraysIR([]*tlSidecar{sc}, rows)
	if !strings.Contains(ir, `ptr @"T.Read$ifn"`) || strings.Contains(ir, `T.Write$ifn`) || !strings.Contains(ir, "ptr null") {
		t.Fatalf("ir:\n%s", ir)
	}
}
