package verify

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSearch runs the directed search on the codec testdata package and
// checks that it confirms the guard mismatches with concrete inputs.
func TestSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a test binary")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
	if !SandboxAvailable() {
		t.Skip("no sandbox")
	}
	r := loadTestdata(t, "codec")
	dir, err := filepath.Abs(filepath.Join("testdata", "src", "codec"))
	if err != nil {
		t.Fatal(err)
	}
	var hints []*Hint
	for _, f := range r.Funcs {
		if h := makeHint(f.Fn, f, r.fset); h != nil {
			hints = append(hints, h)
		}
	}
	hits, err := Search(context.Background(), dir, "codec", hints, SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Names without the package path (analysistest loads testdata as a
	// GOPATH package "codec").
	short := func(s string) string { return s[strings.LastIndex(s, ".")+1:] }
	got := map[string]Hit{}
	for _, h := range hits {
		got[short(h.Fn)+" "+short(h.Obligation)] = h
	}
	for _, want := range []string{"Header ReadU16#idx2", "OffByOne OffByOne#idx1", "GuardedWrong GuardedWrong#idx1", "Arr Arr#idx1"} {
		h, ok := got[want]
		if !ok {
			t.Errorf("no counterexample for %s", want)
			continue
		}
		if h.Runs != 3 {
			t.Errorf("%s: reproduced in %d of 3 runs", want, h.Runs)
		}
	}
	for k := range got {
		for _, safe := range []string{"Sum ", "Guarded ", "Search ", "Rev ", "Mod ", "Pairs "} {
			if strings.HasPrefix(k, safe) {
				t.Errorf("counterexample in a proved function: %s", k)
			}
		}
	}
}
