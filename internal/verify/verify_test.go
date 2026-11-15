package verify

import (
	"encoding/json"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
	"golang.org/x/tools/go/analysis/passes/buildssa"
)

// loadTestdata runs AnalyzePackage on a testdata package.
func loadTestdata(t *testing.T, pkg string) *Result {
	var r *Result
	a := &analysis.Analyzer{
		Name:     "verifyresult",
		Doc:      "result of verify",
		Requires: []*analysis.Analyzer{buildssa.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			prog := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
			r = AnalyzePackage(pass.Fset, pass.Files, prog.SrcFuncs, DefaultOptions)
			r.fset = pass.Fset
			return nil, nil
		},
	}
	// The want comments are for the manifest test; ignore them here.
	analysistest.Run(quiet{}, analysistest.TestData(), a, pkg)
	if r == nil {
		t.Fatal("no result")
	}
	return r
}

type quiet struct{}

func (quiet) Errorf(string, ...any) {}

// manifestOnly reports the manifest findings of Analyzer, the ones the
// testdata marks with want comments.
var manifestOnly = &analysis.Analyzer{
	Name:     "verifymanifest",
	Doc:      "manifest findings of verify",
	Requires: []*analysis.Analyzer{buildssa.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		var out []analysis.Diagnostic
		inner := *pass
		inner.ExportObjectFact = func(types.Object, analysis.Fact) {}
		inner.ImportObjectFact = func(types.Object, analysis.Fact) bool { return false }
		inner.Report = func(d analysis.Diagnostic) {
			kind, payload := DecodeCategory(d.Category)
			if kind != CategoryFinding {
				return
			}
			var in Info
			if err := json.Unmarshal(payload, &in); err != nil {
				pass.Reportf(d.Pos, "bad payload: %v", err)
				return
			}
			if in.Class == "manifest" {
				d.Category = ""
				out = append(out, d)
			}
		}
		if _, err := run(&inner); err != nil {
			return nil, err
		}
		for _, d := range out {
			pass.Report(d)
		}
		return nil, nil
	},
}

func TestManifest(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), manifestOnly, "codec")
}

// TestStatuses checks engine A's status per function on the codec
// package: what it proves and what stays latent.
func TestStatuses(t *testing.T) {
	pkgs := loadTestdata(t, "codec")
	r := pkgs
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
