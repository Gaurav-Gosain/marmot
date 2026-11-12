// Package exhaustive reports switches that do not handle every value of an
// enum-like type, and type switches that do not handle every type of a
// sealed interface: Go's sum types, without new syntax.
//
// An enum is a defined type with an integer or string underlying type and
// at least two package-level constants of exactly that type declared in
// its package. A sealed interface has an unexported method, so only types
// of its own package can implement it; its cases are those types (and
// pointers to them) that do.
//
// A switch with a default clause is exhaustive by definition and is not
// reported. A switch can opt out with a //exhaustive:ignore comment on the
// line before it. The suggested fix adds the missing cases with empty
// bodies, which keeps the behavior of the program (an unhandled value did
// nothing before and does nothing after) and lists them for the reader.
package exhaustive

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer is the exhaustiveness check.
var Analyzer = &analysis.Analyzer{
	Name: "exhaustive",
	Doc:  "report switches that miss values of an enum-like type or types of a sealed interface",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		ignored := ignoreLines(pass.Fset, f)
		ast.Inspect(f, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.SwitchStmt:
				if s.Tag != nil && !ignored[pass.Fset.Position(s.Pos()).Line-1] {
					checkEnum(pass, s)
				}
			case *ast.TypeSwitchStmt:
				if !ignored[pass.Fset.Position(s.Pos()).Line-1] {
					checkSealed(pass, s)
				}
			}
			return true
		})
	}
	return nil, nil
}

func ignoreLines(fset *token.FileSet, f *ast.File) map[int]bool {
	m := map[int]bool{}
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//exhaustive:ignore") {
				m[fset.Position(c.Pos()).Line] = true
			}
		}
	}
	return m
}

// enumMember is one constant of an enum.
type enumMember struct {
	name string
	val  constant.Value
}

// enumMembers returns the constants of named type t declared at package
// level in its package, or nil when t is not enum-like.
func enumMembers(t *types.Named) []enumMember {
	obj := t.Obj()
	if obj.Pkg() == nil {
		return nil
	}
	b, ok := t.Underlying().(*types.Basic)
	if !ok || b.Info()&(types.IsInteger|types.IsString) == 0 {
		return nil
	}
	scope := obj.Pkg().Scope()
	var ms []enumMember
	for _, name := range scope.Names() {
		c, ok := scope.Lookup(name).(*types.Const)
		if !ok || !types.Identical(c.Type(), t) || name == "_" {
			continue
		}
		ms = append(ms, enumMember{name, c.Val()})
	}
	if len(ms) < 2 {
		return nil
	}
	sort.Slice(ms, func(i, j int) bool {
		if constant.Compare(ms[i].val, token.LSS, ms[j].val) {
			return true
		}
		return constant.Compare(ms[i].val, token.EQL, ms[j].val) && ms[i].name < ms[j].name
	})
	return ms
}

func checkEnum(pass *analysis.Pass, s *ast.SwitchStmt) {
	tv, ok := pass.TypesInfo.Types[s.Tag]
	if !ok {
		return
	}
	named, ok := types.Unalias(tv.Type).(*types.Named)
	if !ok {
		return
	}
	members := enumMembers(named)
	if members == nil {
		return
	}
	covered := map[string]bool{} // by exact value
	for _, st := range s.Body.List {
		cc := st.(*ast.CaseClause)
		if cc.List == nil {
			return // default: exhaustive by definition
		}
		for _, e := range cc.List {
			if v := pass.TypesInfo.Types[e].Value; v != nil {
				covered[v.ExactString()] = true
			}
		}
	}
	// Several names can share a value; a value is handled once.
	var missing []string
	seenVal := map[string]bool{}
	for _, m := range members {
		k := m.val.ExactString()
		if covered[k] || seenVal[k] {
			continue
		}
		seenVal[k] = true
		missing = append(missing, qualify(pass, named.Obj(), m.name))
	}
	if len(missing) == 0 {
		return
	}
	report(pass, s.Pos(), s.Body, fmt.Sprintf("switch on %s misses %s", typeName(pass, named), list(missing)), "case "+strings.Join(missing, ", ")+":")
}

func checkSealed(pass *analysis.Pass, s *ast.TypeSwitchStmt) {
	var x ast.Expr
	switch a := s.Assign.(type) {
	case *ast.AssignStmt:
		if len(a.Rhs) == 1 {
			if ta, ok := a.Rhs[0].(*ast.TypeAssertExpr); ok {
				x = ta.X
			}
		}
	case *ast.ExprStmt:
		if ta, ok := a.X.(*ast.TypeAssertExpr); ok {
			x = ta.X
		}
	}
	if x == nil {
		return
	}
	tv, ok := pass.TypesInfo.Types[x]
	if !ok {
		return
	}
	named, ok := types.Unalias(tv.Type).(*types.Named)
	if !ok {
		return
	}
	iface, ok := named.Underlying().(*types.Interface)
	if !ok || !sealed(iface) || named.Obj().Pkg() == nil {
		return
	}
	impls := implementations(named, iface)
	if len(impls) < 2 {
		return
	}
	var cases []types.Type
	for _, st := range s.Body.List {
		cc := st.(*ast.CaseClause)
		if cc.List == nil {
			return
		}
		for _, e := range cc.List {
			if t := pass.TypesInfo.Types[e].Type; t != nil {
				cases = append(cases, t)
			}
		}
	}
	var missing []string
	for _, impl := range impls {
		handled := false
		for _, c := range cases {
			if types.Identical(c, impl) || types.IsInterface(c) && types.Implements(impl, c.Underlying().(*types.Interface)) {
				handled = true
			}
		}
		if !handled {
			missing = append(missing, typeName(pass, impl))
		}
	}
	if len(missing) == 0 {
		return
	}
	report(pass, s.Pos(), s.Body, fmt.Sprintf("type switch on %s misses %s", typeName(pass, named), list(missing)), "case "+strings.Join(missing, ", ")+":")
}

// sealed reports whether an interface has an unexported method.
func sealed(it *types.Interface) bool {
	for i := 0; i < it.NumMethods(); i++ {
		if !it.Method(i).Exported() {
			return true
		}
	}
	return false
}

// implementations lists the named types of the interface's package that
// implement it: T when T has the method set, else *T when that does.
func implementations(named *types.Named, it *types.Interface) []types.Type {
	scope := named.Obj().Pkg().Scope()
	var out []types.Type
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		t, ok := tn.Type().(*types.Named)
		if !ok || types.IsInterface(t) || t.TypeParams().Len() > 0 {
			continue
		}
		switch {
		case types.Implements(t, it):
			out = append(out, t)
		case types.Implements(types.NewPointer(t), it):
			out = append(out, types.NewPointer(t))
		}
	}
	return out
}

func typeName(pass *analysis.Pass, t types.Type) string {
	return types.TypeString(t, types.RelativeTo(pass.Pkg))
}

func qualify(pass *analysis.Pass, typ *types.TypeName, name string) string {
	if typ.Pkg() == pass.Pkg {
		return name
	}
	return typ.Pkg().Name() + "." + name
}

func list(xs []string) string {
	if len(xs) <= 4 {
		return strings.Join(xs, ", ")
	}
	return strings.Join(xs[:4], ", ") + fmt.Sprintf(" and %d more", len(xs)-4)
}

// report adds the diagnostic with a fix that inserts the missing cases
// before the closing brace of the switch.
func report(pass *analysis.Pass, pos token.Pos, body *ast.BlockStmt, msg, clause string) {
	pass.Report(analysis.Diagnostic{
		Pos:     pos,
		End:     body.Lbrace,
		Message: msg,
		SuggestedFixes: []analysis.SuggestedFix{{
			Message:   "add the missing cases (empty: the program does what it did before)",
			TextEdits: []analysis.TextEdit{{Pos: body.Rbrace, End: body.Rbrace, NewText: []byte(clause + "\n")}},
		}},
	})
}
