package verify

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestManifest(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "codec")
}

// TestStatuses checks engine A's status per function on the codec
// package: what it proves and what stays latent.
func TestStatuses(t *testing.T) {
	res := analysistest.Run(t, analysistest.TestData(), Analyzer, "codec")
	r := res[0].Result.(*Result)
	want := map[string]Status{
		"Sum": Proved, "SumRange": Proved, "Pairs": Proved, "Pairs2": Proved,
		"Guarded": Proved, "Mod": Proved, "Div": Proved, "Ptr": Proved,
		"Search": Proved, "Rev": Proved, "Str": Proved, "Back": Proved,
		"At": Latent, "ReadU16": Latent, "Skip": Latent,
	}
	got := map[string]Status{}
	for _, f := range r.Funcs {
		st := Proved
		for _, o := range f.Obligations {
			if o.Via != nil {
				continue
			}
			if o.Status != Proved {
				st = o.Status
			}
		}
		got[f.Fn.Name()] = st
	}
	for fn, w := range want {
		if got[fn] != w {
			t.Errorf("%s: got %s, want %s", fn, got[fn], w)
		}
	}
}
