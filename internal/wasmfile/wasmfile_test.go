package wasmfile

import (
	"bytes"
	"testing"
)

// A module with three exports and a name section: Clean keeps _start and
// memory, drops the name section, adds producers, and the result parses.
func TestClean(t *testing.T) {
	exp := encodeExports([]Export{{"memory", 2, 0}, {"_start", 0, 3}, {"asyncify_get_state", 0, 9}})
	name := appendName(nil, "name")
	name = append(name, 1, 2, 3)
	dbg := appendName(nil, ".debug_info")
	in := Encode([]Section{{1, []byte{0}}, {SecExport, exp}, {SecCustom, name}, {SecCustom, dbg}})
	out, err := Clean(in, Options{Keep: []string{"_start", "memory"}, StripNames: true, Producers: [][2]string{{"marmot", "dev"}}})
	if err != nil {
		t.Fatal(err)
	}
	ss, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 3 || ss[0].ID != 1 || ss[1].ID != SecExport || ss[2].Name() != "producers" {
		t.Fatalf("sections %+v", ss)
	}
	es, err := Exports(ss[1].Data)
	if err != nil || len(es) != 2 || es[0].Name != "memory" || es[1].Name != "_start" || es[1].Index != 3 {
		t.Fatalf("exports %+v %v", es, err)
	}
	if !bytes.Contains(ss[2].Data, []byte("processed-by")) || !bytes.Contains(ss[2].Data, []byte("marmot")) {
		t.Errorf("producers %q", ss[2].Data)
	}
	// Big LEB128 values survive.
	if v, _, _ := uleb(appendUleb(nil, 1<<40+5)); v != 1<<40+5 {
		t.Errorf("leb %d", v)
	}
}
