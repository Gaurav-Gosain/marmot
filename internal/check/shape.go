package check

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// shapeAnalyzer reports, as one finding, a hash of what decides
// reachability in the package: for every function and package level
// declaration, the objects it refers to and the types of its expressions.
// Dead code can only change when some package's shape changes; an edit
// inside a body that refers to the same things (a constant, a comment, a
// condition) keeps it.
var shapeAnalyzer = &analysis.Analyzer{
	Name: "marmotshape",
	Doc:  "hash of the references of every declaration, for the dead code cache",
	Run: func(pass *analysis.Pass) (any, error) {
		var decls []string
		for _, f := range pass.Files {
			for _, d := range f.Decls {
				var b strings.Builder
				switch d := d.(type) {
				case *ast.FuncDecl:
					if d.Recv != nil && len(d.Recv.List) > 0 {
						b.WriteString(types.ExprString(d.Recv.List[0].Type) + ".")
					}
					b.WriteString(d.Name.Name)
				case *ast.GenDecl:
					b.WriteString(d.Tok.String())
					for _, sp := range d.Specs {
						switch sp := sp.(type) {
						case *ast.ValueSpec:
							for _, n := range sp.Names {
								b.WriteString(" " + n.Name)
							}
						case *ast.TypeSpec:
							b.WriteString(" " + sp.Name.Name)
						}
					}
				}
				refs := map[string]bool{}
				ast.Inspect(d, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.Ident:
						if o := pass.TypesInfo.Uses[n]; o != nil {
							refs["u "+objString(o)] = true
						}
					case ast.Expr:
						if t := pass.TypesInfo.TypeOf(n); t != nil {
							refs["t "+t.String()] = true
						}
					}
					return true
				})
				keys := make([]string, 0, len(refs))
				for k := range refs {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				b.WriteString("\x00" + strings.Join(keys, "\x00"))
				decls = append(decls, b.String())
			}
		}
		sort.Strings(decls)
		h := newHasher("shape")
		h.str(decls...)
		if len(pass.Files) > 0 {
			pass.Report(analysis.Diagnostic{Pos: pass.Files[0].Package, Message: h.sum()})
		}
		return nil, nil
	},
}

func objString(o types.Object) string {
	if o.Pkg() != nil {
		return o.Pkg().Path() + "." + o.Name() + " " + o.Type().String()
	}
	return o.Name()
}
