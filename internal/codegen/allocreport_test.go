package codegen

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// escapeWhy restates the cases of escapesF to name the escaping use. Over
// the run/core programs it must agree with escapes on every heap Alloc and
// MakeClosure: a reason exactly when the value escapes.
func TestEscapeWhyAgrees(t *testing.T) {
	files, _ := filepath.Glob("../../tests/run/core/*.go")
	if len(files) == 0 {
		t.Skip("no run/core programs")
	}
	sizes := types.SizesFor("gc", "arm64")
	checked := 0
	for _, path := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}
		pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default(), Error: func(error) {}}, fset,
			types.NewPackage("main", ""), []*ast.File{f}, ssa.InstantiateGenerics)
		if err != nil {
			continue // programs with imports export data cannot provide
		}
		p := &Program{opts: Options{Sizes: sizes}}
		for fn := range ssautil.AllFunctions(pkg.Prog) {
			if fn.Pkg != pkg {
				continue
			}
			for _, b := range fn.Blocks {
				for _, instr := range b.Instrs {
					var v ssa.Value
					switch x := instr.(type) {
					case *ssa.Alloc:
						if x.Heap {
							v = x
						}
					case *ssa.MakeClosure:
						v = x
					}
					if v == nil {
						continue
					}
					loop := inLoop(b)
					esc := p.escapes(v, map[ssa.Value]bool{}, loop)
					why := p.escapeWhy(v, map[ssa.Value]bool{}, loop)
					if esc != (why != "") {
						t.Errorf("%s: %s in %s: escapes=%v, escapeWhy=%q", filepath.Base(path), v.Name(), fn, esc, why)
					}
					checked++
				}
			}
		}
	}
	if checked == 0 {
		t.Skip("no heap allocations checked")
	}
	t.Logf("%d allocation sites checked", checked)
}
