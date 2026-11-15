package driver

import (
	"slices"
	"testing"
)

// The table of a wasip1 program comes from the final module, with the
// lines of wasm-opt's output source map when marmot-pcln wrote an input map.
func TestWasmPclnArgs(t *testing.T) {
	tbl := []string{"-o", "pcln.bin", "-main-pkg=example.com/p"}
	got := wasmPclnArgs(tbl, "final.map", "prog.wasm")
	want := []string{"-wasm-final", "-source-map", "final.map", "-o", "pcln.bin", "-main-pkg=example.com/p", "prog.wasm"}
	if !slices.Equal(got, want) {
		t.Errorf("with a map: %q, want %q", got, want)
	}
	got = wasmPclnArgs(tbl, "", "prog.wasm")
	want = []string{"-wasm-final", "-o", "pcln.bin", "-main-pkg=example.com/p", "prog.wasm"}
	if !slices.Equal(got, want) {
		t.Errorf("without a map: %q, want %q", got, want)
	}
}
