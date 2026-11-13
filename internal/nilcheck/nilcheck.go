// Package nilcheck reports dereferences of pointers and writes to maps
// that can be nil, across functions and packages.
//
// Two sources of nil are tracked on the SSA form:
//
//   - a variable that is nil on one path into the use: var p *T; if c { p =
//     &T{} }; p.f (a phi with a nil edge);
//   - a function result that the function returns as nil with every error
//     result nil and every bool result true at the same return (return nil,
//     nil; return nil, true; or a single pointer result returned as nil).
//     A non-nil error or a false ok marks the nil as expected. Such
//     functions get a fact, so callers in other packages see it too.
//
// A use is safe when a comparison of the value with nil that dominates it
// sends control to it only on the non-nil side. A nil on one path is only
// reported when the use follows the merge without another branch in
// between, since such a branch often depends on a correlated condition. Only dereferences
// (loads, stores, field and element addresses) and map writes are checked;
// method calls on nil receivers are legal Go and are not reported.
package nilcheck

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

// Analyzer is the nil check.
var Analyzer = &analysis.Analyzer{
	Name:      "nilcheck",
	Doc:       "report dereferences of pointers and writes to maps that can be nil, across functions and packages",
	Run:       run,
	Requires:  []*analysis.Analyzer{buildssa.Analyzer},
	FactTypes: []analysis.Fact{new(nilResults)},
}

// nilResults is the fact of a function that can return nil (with nil
// errors) for some results.
type nilResults struct {
	Index []int    // result indices
	Where []string // file:line of a return that returns nil there
}

func (*nilResults) AFact() {}

func (f *nilResults) String() string { return fmt.Sprintf("nilResults%v", f.Index) }

func run(pass *analysis.Pass) (any, error) {
	prog := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	// Facts first, for calls within the package too.
	for _, fn := range prog.SrcFuncs {
		if f := returnsNil(pass, fn); f != nil {
			if obj, ok := fn.Object().(*types.Func); ok && fn.Parent() == nil {
				pass.ExportObjectFact(obj, f)
			}
		}
	}
	for _, fn := range prog.SrcFuncs {
		checkFunc(pass, fn)
	}
	return nil, nil
}

// returnsNil computes the fact for fn.
func returnsNil(pass *analysis.Pass, fn *ssa.Function) *nilResults {
	res := fn.Signature.Results()
	if res.Len() == 0 {
		return nil
	}
	errorType := types.Universe.Lookup("error").Type()
	f := &nilResults{}
	seen := map[int]bool{}
	for _, b := range fn.Blocks {
		ret, ok := b.Instrs[len(b.Instrs)-1].(*ssa.Return)
		if !ok {
			continue
		}
		// Result i being nil is unexpected only when no other result
		// signals failure: a non-nil error, a false ok, or any other
		// non-nil pointer, interface or reference (a custom error struct,
		// the value that is present instead).
		for i, v := range ret.Results {
			if seen[i] || !nilable(res.At(i).Type()) || !isNilConst(v) {
				continue
			}
			failure := false
			for j, w := range ret.Results {
				if j == i {
					continue
				}
				t := res.At(j).Type()
				if b, ok := t.Underlying().(*types.Basic); ok && b.Kind() == types.Bool {
					if c, ok := w.(*ssa.Const); !ok || c.Value == nil || c.Value.String() != "true" {
						failure = true
					}
				}
				if (refLike(t) || isErrorType(t, errorType)) && !isNilConst(w) {
					failure = true
				}
			}
			if !failure {
				seen[i] = true
				f.Index = append(f.Index, i)
				f.Where = append(f.Where, posString(pass.Fset, ret.Pos()))
			}
		}
	}
	if len(f.Index) == 0 {
		return nil
	}
	return f
}

// refLike reports whether t is a pointer, interface, map, slice, channel
// or function type.
func refLike(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface, *types.Map, *types.Slice, *types.Chan, *types.Signature:
		return true
	}
	return false
}

// nilable reports whether values of t can be dereferenced or written
// through: pointers and maps.
func nilable(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Map:
		return true
	}
	return false
}

// isErrorType reports whether t is error or a type that implements it
// (custom error types count as error results).
func isErrorType(t, errorType types.Type) bool {
	if types.Identical(t, errorType) {
		return true
	}
	it := errorType.Underlying().(*types.Interface)
	return types.Implements(t, it) || types.Implements(types.NewPointer(t), it) && !types.IsInterface(t)
}

func isNilConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.IsNil()
}

func posString(fset *token.FileSet, p token.Pos) string {
	pos := fset.Position(p)
	if !pos.IsValid() {
		return "?"
	}
	return fmt.Sprintf("%s:%d", shortFile(pos.Filename), pos.Line)
}

func shortFile(p string) string {
	if _, after, ok := strings.CutLast(p, "/"); ok {
		return after
	}
	return p
}

// checkFunc reports the unsafe uses in fn.
func checkFunc(pass *analysis.Pass, fn *ssa.Function) {
	if strings.HasSuffix(pass.Fset.Position(fn.Pos()).Filename, "_test.go") {
		return // tests know their data; reports there are mostly noise
	}
	reported := map[ssa.Value]bool{}
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			var ptr ssa.Value
			what := "dereferenced"
			switch x := instr.(type) {
			case *ssa.UnOp:
				if x.Op == token.MUL {
					ptr = x.X
				}
			case *ssa.Store:
				ptr = x.Addr
			case *ssa.FieldAddr:
				ptr = x.X
			case *ssa.IndexAddr:
				if _, ok := x.X.Type().Underlying().(*types.Pointer); ok {
					ptr = x.X
				}
			case *ssa.MapUpdate:
				ptr, what = x.Map, "written to (a nil map panics on write)"
			}
			if ptr == nil || reported[ptr] {
				continue
			}
			why := mayBeNil(pass, ptr)
			if why == "" || guarded(ptr, b) {
				continue
			}
			if phi, ok := ptr.(*ssa.Phi); ok && !straightAfter(phi.Block(), b) {
				// A branch between the merge and the use may depend on
				// a condition correlated with the nil path (if n == 1
				// after counting matches): too uncertain to report.
				continue
			}
			reported[ptr] = true
			pos := instr.Pos()
			if !pos.IsValid() {
				pos = ptr.Pos()
			}
			if !pos.IsValid() {
				continue
			}
			pass.Report(analysis.Diagnostic{
				Pos:     pos,
				Message: fmt.Sprintf("%s may be nil when %s: %s", describe(ptr), what, why),
			})
		}
	}
}

// mayBeNil says why v can be nil, or "".
func mayBeNil(pass *analysis.Pass, v ssa.Value) string {
	switch x := v.(type) {
	case *ssa.Phi:
		for i, e := range x.Edges {
			if isNilConst(e) {
				pred := x.Block().Preds[i]
				where := ""
				if p := lastPos(pred); p.IsValid() {
					where = " (coming from line " + fmt.Sprint(pass.Fset.Position(p).Line) + ")"
				}
				return "it is nil on one path into this use" + where
			}
		}
	case *ssa.Extract:
		if c, ok := x.Tuple.(*ssa.Call); ok {
			if why := callReturnsNil(pass, c, x.Index); why != "" {
				return why
			}
		}
	case *ssa.Call:
		return callReturnsNil(pass, x, 0)
	}
	return ""
}

func lastPos(b *ssa.BasicBlock) token.Pos {
	for _, v := range slices.Backward(b.Instrs) {
		if p := v.Pos(); p.IsValid() {
			return p
		}
	}
	return token.NoPos
}

func callReturnsNil(pass *analysis.Pass, c *ssa.Call, index int) string {
	callee := c.Call.StaticCallee()
	if callee == nil {
		return ""
	}
	obj, ok := callee.Object().(*types.Func)
	if !ok {
		return ""
	}
	var f nilResults
	if !pass.ImportObjectFact(obj, &f) {
		return ""
	}
	for i, idx := range f.Index {
		if idx == index {
			name := obj.Name()
			if obj.Pkg() != nil && obj.Pkg() != pass.Pkg {
				name = obj.Pkg().Name() + "." + name
			}
			return fmt.Sprintf("%s returns nil for it with a nil error at %s", name, f.Where[i])
		}
	}
	return ""
}

func describe(v ssa.Value) string {
	if n := v.Name(); n != "" && !strings.HasPrefix(n, "t") {
		return n
	}
	switch x := v.(type) {
	case *ssa.Extract:
		if c, ok := x.Tuple.(*ssa.Call); ok {
			if f := c.Call.StaticCallee(); f != nil {
				return fmt.Sprintf("the result of %s", f.Name())
			}
		}
	case *ssa.Call:
		if f := x.Call.StaticCallee(); f != nil {
			return fmt.Sprintf("the result of %s", f.Name())
		}
	case *ssa.Phi:
		if c := x.Comment; c != "" {
			return c
		}
	}
	return "the value"
}

// guarded reports whether a dominating "v != nil" (or "v == nil")
// comparison lets control reach b only when v is not nil.
func guarded(v ssa.Value, b *ssa.BasicBlock) bool {
	for d := b; d != nil; d = d.Idom() {
		idom := d.Idom()
		if idom == nil {
			break
		}
		ifi, ok := idom.Instrs[len(idom.Instrs)-1].(*ssa.If)
		if !ok {
			continue
		}
		cmp, ok := ifi.Cond.(*ssa.BinOp)
		if !ok || (cmp.Op != token.EQL && cmp.Op != token.NEQ) {
			continue
		}
		if (cmp.X != v || !isNilConst(cmp.Y)) && (cmp.Y != v || !isNilConst(cmp.X)) {
			continue
		}
		// Succs[0] is the true branch. For v != nil, the non-nil side is
		// Succs[0]; for v == nil, Succs[1].
		safe := idom.Succs[0]
		if cmp.Op == token.EQL {
			safe = idom.Succs[1]
		}
		if dominates(safe, b) {
			return true
		}
	}
	return false
}

// straightAfter reports whether b is reached from a through blocks that
// end in unconditional jumps only (or is a).
func straightAfter(a, b *ssa.BasicBlock) bool {
	for b != a {
		if len(b.Preds) != 1 {
			return false
		}
		p := b.Preds[0]
		if _, isJump := p.Instrs[len(p.Instrs)-1].(*ssa.Jump); !isJump {
			return false
		}
		b = p
	}
	return true
}

func dominates(a, b *ssa.BasicBlock) bool {
	for x := b; x != nil; x = x.Idom() {
		if x == a {
			return true
		}
	}
	return false
}
