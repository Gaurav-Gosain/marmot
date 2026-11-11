package specialize

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/astutil"
)

// Map keys built only to index a map.
//
// m[strings.ToUpper(w)]++ (and --, +=, -=, ToLower) allocates the key
// string even when the key is already in the map. The rewrite upper-cases
// (or lower-cases) an ASCII w into a stack buffer and calls the runtime's
// mapassign_faststr_bytes, which returns the element slot and allocates
// the key string only when the key is new. Non-ASCII or long words, and nil
// maps, take the original statement. Evaluation order is kept: m, then w,
// then the delta, then the case conversion (which has no effects).

func rewriteMapKey(g *gen, f *ast.File) {
	if !g.on["mapkey"] {
		return
	}
	if !runtimeHas(g, "mapassign_faststr_bytes") {
		return
	}
	astutil.Apply(f, nil, func(c *astutil.Cursor) bool {
		var idx ast.Expr
		var delta ast.Expr
		op := "+="
		switch s := c.Node().(type) {
		case *ast.IncDecStmt:
			idx = s.X
			delta = &ast.BasicLit{Kind: token.INT, Value: "1"}
			if s.Tok == token.DEC {
				op = "-="
			}
		case *ast.AssignStmt:
			if len(s.Lhs) != 1 || len(s.Rhs) != 1 || (s.Tok != token.ADD_ASSIGN && s.Tok != token.SUB_ASSIGN) {
				return true
			}
			idx, delta = s.Lhs[0], s.Rhs[0]
			if s.Tok == token.SUB_ASSIGN {
				op = "-="
			}
		default:
			return true
		}
		ie, ok := ast.Unparen(idx).(*ast.IndexExpr)
		if !ok {
			return true
		}
		mt, ok := g.info.Types[ie.X].Type.Underlying().(*types.Map)
		if !ok || !types.Identical(mt.Key(), types.Typ[types.String]) {
			return true
		}
		vb, ok := mt.Elem().Underlying().(*types.Basic)
		if !ok || vb.Info()&(types.IsInteger|types.IsFloat) == 0 {
			return true
		}
		call, ok := ast.Unparen(ie.Index).(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() {
			return true
		}
		fn := calleeFunc(g.info, call)
		if fn == nil || fn.Pkg().Path() != "strings" || (fn.Name() != "ToUpper" && fn.Name() != "ToLower") {
			return true
		}
		msrc, vsrc := g.typeExpr(g.info.Types[ie.X].Type), g.typeExpr(mt.Elem())
		if msrc == "" || vsrc == "" {
			return true
		}
		lo, hi, shift := "'a'", "'z'", "-= 'a' - 'A'"
		if fn.Name() == "ToLower" {
			lo, hi, shift = "'A'", "'Z'", "+= 'a' - 'A'"
		}
		name := g.newName("mapkey")
		g.decls = append(g.decls, fmt.Sprintf(`func %[1]s(m %[2]s, w string, d %[3]s) {
	if m == nil || len(w) > 64 {
		m[strings.%[4]s(w)] %[8]s d
		return
	}
	var buf [64]byte
	for i := 0; i < len(w); i++ {
		c := w[i]
		if c >= 0x80 {
			m[strings.%[4]s(w)] %[8]s d
			return
		}
		if %[5]s <= c && c <= %[6]s {
			c %[7]s
		}
		buf[i] = c
	}
	e := any(m)
	ew := (*[2]unsafe.Pointer)(unsafe.Pointer(&e))
	p := marmot_mapassign_faststr_bytes(ew[0], ew[1], buf[:len(w)])
	*(*%[3]s)(p) %[8]s d
}
`, name, msrc, vsrc, fn.Name(), lo, hi, shift, op))
		g.usePkg("strings", "strings")
		g.importAs("unsafe", "unsafe")
		g.needMapBytes = true
		orig := c.Node()
		repl := &ast.ExprStmt{X: &ast.CallExpr{Fun: ast.NewIdent(name), Args: []ast.Expr{ie.X, call.Args[0], delta}}}
		c.Replace(repl)
		g.sites++
		g.undos = append(g.undos, func() {
			astutil.Apply(f, nil, func(c2 *astutil.Cursor) bool {
				if c2.Node() == repl {
					c2.Replace(orig)
				}
				return true
			})
		})
		return true
	})
}

// runtimeHas reports whether the loaded runtime package defines name.
func runtimeHas(g *gen, name string) bool {
	for _, p := range g.allPkgs {
		if p.PkgPath == "runtime" && p.Types != nil {
			return p.Types.Scope().Lookup(name) != nil
		}
	}
	return false
}
