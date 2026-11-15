package verify

import (
	"fmt"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
)

// Analyzer runs tier 1 on a package. It reports manifest findings (guard
// mismatches), and with -latent the latent obligations too. Its result is
// the *Result with every obligation's status.
var Analyzer = &analysis.Analyzer{
	Name:       "verify",
	Doc:        "find run-time panics that a guard in the code fails to exclude, and prove the absence of the others",
	Run:        run,
	Requires:   []*analysis.Analyzer{buildssa.Analyzer},
	ResultType: reflect.TypeFor[*Result](),
}

var latentFlag bool

func init() {
	Analyzer.Flags.BoolVar(&latentFlag, "latent", false, "also report latent panics (preconditions on the inputs)")
}

func run(pass *analysis.Pass) (any, error) {
	prog := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	r := AnalyzePackage(pass.Fset, pass.Files, prog.SrcFuncs, DefaultOptions)
	idx := newSpanIndex(pass.Files)
	for _, f := range r.Funcs {
		for _, o := range f.Obligations {
			switch {
			case o.Class == "manifest":
			case o.Status == Latent && latentFlag:
			default:
				continue
			}
			pos, end := idx.span(o.Ob.Pos)
			d := analysis.Diagnostic{Pos: pos, End: end, Category: o.Class, Message: Message(f.Fn, o.Ob)}
			if o.Condition != "" {
				d.Message += fmt.Sprintf(" when %s", o.Condition)
			}
			for _, l := range o.Labels {
				lp, le := idx.span(l.Pos)
				d.Related = append(d.Related, analysis.RelatedInformation{Pos: lp, End: le, Message: l.Text})
			}
			pass.Report(d)
		}
	}
	return r, nil
}
