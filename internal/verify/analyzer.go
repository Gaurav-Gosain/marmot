package verify

import (
	"fmt"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
)

// Analyzer runs tier 1 on a package. It reports manifest findings (guard
// mismatches) and latent obligations, each with its Info in the
// category (CategoryFinding), and one statistics diagnostic
// (CategoryStats) per package. Consumers choose what to show: latent
// findings are notes and off by default. Its result is the *Result with
// every obligation's status.
var Analyzer = &analysis.Analyzer{
	Name:       "verify",
	Doc:        "find run-time panics that a guard in the code fails to exclude, and prove the absence of the others",
	Run:        run,
	Requires:   []*analysis.Analyzer{buildssa.Analyzer},
	ResultType: reflect.TypeFor[*Result](),
}

func run(pass *analysis.Pass) (any, error) {
	prog := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	r := AnalyzePackage(pass.Fset, pass.Files, prog.SrcFuncs, DefaultOptions)
	idx := newSpanIndex(pass.Files)
	for _, f := range r.Funcs {
		for _, o := range f.Obligations {
			if o.Class != "manifest" && o.Status != Latent {
				continue
			}
			pos, end := idx.span(o.Ob.Pos)
			msg := Message(f.Fn, o.Ob)
			if o.Condition != "" {
				msg += fmt.Sprintf(" when %s", o.Condition)
			}
			d := analysis.Diagnostic{Pos: pos, End: end, Category: encodeCategory(CategoryFinding, info(f, o)), Message: msg}
			for _, l := range o.Labels {
				lp, le := idx.span(l.Pos)
				d.Related = append(d.Related, analysis.RelatedInformation{Pos: lp, End: le, Message: l.Text})
			}
			pass.Report(d)
		}
	}
	if len(pass.Files) > 0 {
		pass.Report(analysis.Diagnostic{Pos: pass.Files[0].Package, Category: encodeCategory(CategoryStats, r.Summary()), Message: "verify statistics"})
	}
	return r, nil
}
