package verify

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"

	"golang.org/x/tools/go/ssa"

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
	FactTypes:  []analysis.Fact{new(FnSummary)},
}

func run(pass *analysis.Pass) (any, error) {
	prog := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	opts := DefaultOptions
	opts.Summaries = func(f *ssa.Function) *FnSummary {
		obj, ok := f.Object().(*types.Func)
		if !ok || f.Parent() != nil || obj.Pkg() == nil || obj.Pkg() == pass.Pkg {
			return nil
		}
		var s FnSummary
		if pass.ImportObjectFact(obj, &s) {
			return &s
		}
		return nil
	}
	r := AnalyzePackage(pass.Fset, pass.Files, prog.SrcFuncs, opts)
	for f, s := range r.Summaries {
		if obj, ok := f.Object().(*types.Func); ok && f.Parent() == nil && obj.Pkg() == pass.Pkg && s != nil {
			pass.ExportObjectFact(obj, s)
		}
	}
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
			} else if o.Rule == "always" {
				msg += " on every input"
			}
			in := info(f, o)
			in.Label = primaryLabel(idx, o)
			d := analysis.Diagnostic{Pos: pos, End: end, Category: encodeCategory(CategoryFinding, in), Message: msg}
			for _, l := range o.Labels {
				lp, le := idx.span(l.Pos)
				d.Related = append(d.Related, analysis.RelatedInformation{Pos: lp, End: le, Message: l.Text})
			}
			pass.Report(d)
		}
	}
	for _, f := range r.Funcs {
		if h := makeHint(f.Fn, f, pass.Fset); h != nil {
			pass.Report(analysis.Diagnostic{Pos: f.Fn.Pos(), Category: encodeCategory(CategorySearch, h), Message: "verify search hint"})
		}
	}
	if len(pass.Files) > 0 {
		pass.Report(analysis.Diagnostic{Pos: pass.Files[0].Package, Category: encodeCategory(CategoryStats, r.Summary()), Message: "verify statistics"})
	}
	return r, nil
}

// primaryLabel is the text under the primary span, which must make sense
// alone: the expression, what goes wrong, and when.
func primaryLabel(idx *spanIndex, o *ObResult) string {
	expr := ""
	if n, ok := idx.at[o.Ob.Pos]; ok {
		if e, ok := n.(ast.Expr); ok {
			expr = types.ExprString(e)
		}
	}
	var s string
	switch o.Ob.Kind {
	case KIndex:
		s = "index out of range"
		if expr != "" {
			s = expr + " is out of range"
		}
	case KSlice:
		s = "slice bounds out of range"
		if expr != "" {
			s = expr + " is out of bounds"
		}
	case KDivide:
		s = "division by zero"
	case KNil:
		s = "nil dereference"
		if expr != "" {
			s = expr + " dereferences nil"
		}
	default:
		s = panicText(o.Ob)
	}
	if o.Condition != "" {
		s += " when " + o.Condition
	}
	return s
}
