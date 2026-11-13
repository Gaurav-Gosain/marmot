package specialize

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Formatting straight into a builder.
//
// sb.WriteString(strconv.Itoa(x)) as a statement, on a *strings.Builder or
// *bytes.Buffer (or an addressable value of those types), builds a string
// only to copy it. It becomes
//
//	{ v := x; var b [32]byte; sb.Write(strconv.AppendInt(b[:0], int64(v), 10)) }
//
// The operands are evaluated first, as before, then the digits are appended
// into a stack scratch array and written: the
// same bytes, and the builder grows exactly as WriteString would (Write and
// WriteString grow the same way for the same length). The receiver must be
// a plain variable or field path, since it is evaluated twice.

// appendForms maps a strconv formatter to its Append form and the
// conversions its arguments need.
var appendForms = map[string]struct {
	fn   string
	args []string // conversion per argument ("" for none); Itoa adds base 10
}{
	"Itoa":        {"AppendInt", []string{"int64"}},
	"FormatInt":   {"AppendInt", []string{"", ""}},
	"FormatUint":  {"AppendUint", []string{"", ""}},
	"FormatFloat": {"AppendFloat", []string{"", "", "", ""}},
	"FormatBool":  {"AppendBool", []string{""}},
	"Quote":       {"AppendQuote", []string{""}},
}

func rewriteBuilder(g *gen, f *ast.File) {
	if !g.on["builder"] {
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		es, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() {
			return true
		}
		if !isMethod(g.info, call, "strings", "Builder", "WriteString") && !isMethod(g.info, call, "bytes", "Buffer", "WriteString") {
			return true
		}
		recv := ast.Unparen(call.Fun).(*ast.SelectorExpr).X
		if _, ok := ast.Unparen(recv).(*ast.Ident); !ok {
			return true // evaluated twice; a field path through a nil pointer would panic at another point
		}
		inner, ok := ast.Unparen(call.Args[0]).(*ast.CallExpr)
		if !ok || inner.Ellipsis.IsValid() {
			return true
		}
		fn := calleeFunc(g.info, inner)
		if fn == nil || fn.Pkg().Path() != "strconv" {
			return true
		}
		form, ok := appendForms[fn.Name()]
		if !ok || len(inner.Args) != len(form.args) {
			return true
		}
		sel, ok := ast.Unparen(inner.Fun).(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgName, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		// { v0 := a0; ...; b := recv.AvailableBuffer(); recv.Write(strconv.AppendX(b, v0, ...)) }
		var stmts []ast.Stmt
		appendArgs := []ast.Expr{}
		buf := ast.NewIdent(g.newName("b"))
		appendArgs = append(appendArgs, &ast.SliceExpr{X: buf, High: &ast.BasicLit{Kind: token.INT, Value: "0"}})
		params := fn.Type().(*types.Signature).Params()
		for i, a := range inner.Args {
			v := ast.NewIdent(g.newName("v"))
			// Convert to the parameter type, so untyped constants keep the
			// type they had as arguments.
			pt := ast.NewIdent(types.TypeString(params.At(i).Type(), nil))
			stmts = append(stmts, &ast.AssignStmt{Lhs: []ast.Expr{v}, Tok: token.DEFINE, Rhs: []ast.Expr{&ast.CallExpr{Fun: pt, Args: []ast.Expr{a}}}})
			var e ast.Expr = v
			if form.args[i] != "" {
				e = &ast.CallExpr{Fun: ast.NewIdent(form.args[i]), Args: []ast.Expr{v}}
			}
			appendArgs = append(appendArgs, e)
		}
		if fn.Name() == "Itoa" {
			appendArgs = append(appendArgs, &ast.BasicLit{Kind: token.INT, Value: "10"})
		}
		stmts = append(stmts,
			// var b [32]byte (a stack scratch; AppendX moves to the heap
			// only for longer results)
			&ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{
				Names: []*ast.Ident{buf},
				Type:  &ast.ArrayType{Len: &ast.BasicLit{Kind: token.INT, Value: "32"}, Elt: ast.NewIdent("byte")},
			}}}},
			&ast.ExprStmt{X: &ast.CallExpr{
				Fun: &ast.SelectorExpr{X: recv, Sel: ast.NewIdent("Write")},
				Args: []ast.Expr{&ast.CallExpr{
					Fun:  &ast.SelectorExpr{X: ast.NewIdent(pkgName.Name), Sel: ast.NewIdent(form.fn)},
					Args: appendArgs,
				}},
			}},
		)
		g.replaceStmt(es, &ast.BlockStmt{List: stmts})
		return true
	})
}
