package specialize

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// Ranging over a split.
//
// `for [i,] f := range strings.Split(s, sep)` with a constant non-empty
// sep allocates the []string only to walk it. It becomes a loop over
// strings.Cut that yields the same elements in the same order (Split's
// elements are exactly the successive Cut results; the last one is what is
// left when no separator remains, so "" splits to [""]). s and sep are
// evaluated once before the loop, as the range expression is, and f (and i)
// are declared inside the body, so each iteration has its own variables.
// `for [_,] f := range strings.Fields(s)` becomes strings.FieldsSeq, which
// yields the same fields. Only strings: bytes.Split caps each subslice, a Cut
// loop would not.

func rewriteSplit(g *gen, f *ast.File) {
	if !g.on["split"] {
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		rs, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		call, ok := ast.Unparen(rs.X).(*ast.CallExpr)
		if !ok || call.Ellipsis.IsValid() {
			return true
		}
		fn := calleeFunc(g.info, call)
		if fn == nil || fn.Pkg().Path() != "strings" {
			return true
		}
		switch {
		case fn.Name() == "Split" && len(call.Args) == 2:
			tv := g.info.Types[call.Args[1]]
			if tv.Value == nil || tv.Value.Kind() != constant.String || constant.StringVal(tv.Value) == "" {
				return true
			}
			g.splitLoop(rs, call)
		case fn.Name() == "Fields" && len(call.Args) == 1:
			g.fieldsLoop(rs, call)
		}
		return true
	})
}

// isBlank reports whether e is absent or the blank identifier.
func isBlank(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return e == nil || ok && id.Name == "_"
}

func (g *gen) splitLoop(rs *ast.RangeStmt, call *ast.CallExpr) {
	// Shapes: for range X; for i := range X; for i, f := range X; for _, f := range X.
	// Assignments to existing variables (=) are left alone.
	if rs.Key != nil && rs.Tok != token.DEFINE {
		return
	}
	if !isBlank(rs.Value) {
		if _, ok := rs.Value.(*ast.Ident); !ok {
			return
		}
	}
	if !isBlank(rs.Key) {
		if _, ok := rs.Key.(*ast.Ident); !ok {
			return
		}
	}
	rest := ast.NewIdent(g.newName("rest"))
	more := ast.NewIdent(g.newName("more"))
	sep := ast.NewIdent(g.newName("sep"))
	idx := ast.NewIdent(g.newName("i"))
	elem := ast.NewIdent(g.newName("f"))
	// for rest, more, sep, i := s, true, sep, -1; more; {
	//	i++
	//	var elem string
	//	elem, rest, more = strings.Cut(rest, sep)
	//	key, value := i, elem   (as declared by the range)
	//	body
	// }
	cut := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent(g.stringsName()), Sel: ast.NewIdent("Cut")}, Args: []ast.Expr{rest, sep}}
	if g.stringsName() == "" {
		return
	}
	pre := []ast.Stmt{
		&ast.IncDecStmt{X: idx, Tok: token.INC},
		&ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{elem}, Type: ast.NewIdent("string")}}}},
		&ast.AssignStmt{Lhs: []ast.Expr{elem, rest, more}, Tok: token.ASSIGN, Rhs: []ast.Expr{cut}},
	}
	var lhs, rhs []ast.Expr
	if !isBlank(rs.Key) {
		lhs, rhs = append(lhs, rs.Key), append(rhs, idx)
	}
	if !isBlank(rs.Value) {
		lhs, rhs = append(lhs, rs.Value), append(rhs, elem)
	}
	if len(lhs) > 0 {
		pre = append(pre, &ast.AssignStmt{Lhs: lhs, Tok: token.DEFINE, Rhs: rhs})
	}
	pre = append(pre, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("_"), ast.NewIdent("_")}, Tok: token.ASSIGN, Rhs: []ast.Expr{idx, elem}})
	body := &ast.BlockStmt{Lbrace: rs.Body.Lbrace, List: append(pre, &ast.BlockStmt{List: rs.Body.List}), Rbrace: rs.Body.Rbrace}
	loop := &ast.ForStmt{
		For: rs.For,
		Init: &ast.AssignStmt{
			Lhs: []ast.Expr{rest, more, sep, idx}, Tok: token.DEFINE,
			Rhs: []ast.Expr{call.Args[0], ast.NewIdent("true"), call.Args[1], &ast.BasicLit{Kind: token.INT, Value: "-1"}},
		},
		Cond: more,
		Body: body,
	}
	g.replaceStmt(rs, loop)
}

func (g *gen) fieldsLoop(rs *ast.RangeStmt, call *ast.CallExpr) {
	// Only value loops without an index: for _, f := range strings.Fields(s).
	if !isBlank(rs.Key) || rs.Value == nil || rs.Tok != token.DEFINE {
		return
	}
	if _, ok := rs.Value.(*ast.Ident); !ok {
		return
	}
	sp := g.pkg.Imports["strings"]
	if sp == nil || sp.Types.Scope().Lookup("FieldsSeq") == nil || g.stringsName() == "" {
		return
	}
	seq := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent(g.stringsName()), Sel: ast.NewIdent("FieldsSeq")}, Args: call.Args}
	loop := &ast.RangeStmt{For: rs.For, Key: rs.Value, Tok: token.DEFINE, Range: rs.Range, X: seq, Body: rs.Body}
	g.replaceStmt(rs, loop)
}

// stringsName returns the name under which the current file refers to
// package strings ("" if the file being rewritten has no such import; the
// caller only gets here from a strings.X call, so it normally has one).
func (g *gen) stringsName() string {
	for id, obj := range g.info.Uses {
		if pn, ok := obj.(*types.PkgName); ok && pn.Imported().Path() == "strings" && g.curFile != nil &&
			id.Pos() >= g.curFile.Pos() && id.Pos() < g.curFile.End() {
			return id.Name
		}
	}
	return ""
}
