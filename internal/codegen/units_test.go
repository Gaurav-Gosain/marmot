package codegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// The analyses that look into callees (escape, region, frame region and
// panic summaries) must reach a callee only through calleeFor or sameUnit:
// in separate compilation another package's body is unknown, whether or not
// it is loaded (sameUnit). tests/incremental checks the result end to end.
func TestAnalysesUseCalleeFor(t *testing.T) {
	fset := token.NewFileSet()
	for _, name := range []string{"escape.go", "frameregion.go", "panics.go", "region.go"} {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "StaticCallee" {
					t.Errorf("%s: %s calls StaticCallee; use calleeFor", fset.Position(sel.Pos()), fd.Name.Name)
				}
				return true
			})
		}
	}
}

// Whether a package is importable in the SSA program depends on how the
// incremental loader loaded it, so code generation must not ask.
func TestNoImportedPackage(t *testing.T) {
	names, _ := filepath.Glob("*.go")
	for _, name := range names {
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "ImportedPackage" {
				t.Errorf("%s calls ImportedPackage", name)
			}
			return true
		})
	}
}
