package specialize

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"go/version"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// sort specialization.
//
// sort.Slice, sort.SliceStable, sort.Sort and sort.Stable run the very
// algorithm of the standard library (zsortfunc.go and zsortinterface.go,
// taken from the GOROOT being compiled against) made generic over the
// element type and the comparator, so the instantiated code compares and
// swaps in exactly the same sequence as the general path, with direct
// swaps instead of reflectlite.Swapper and a comparator the backend can
// inline. A less function literal whose captured variables can be named at
// package level becomes a method on a struct of pointers to those variables;
// any other less function is called through a func value as before.

// sortTemplates holds the generic copies of the sort algorithms for one
// package, emitted once.
type sortTemplates struct {
	done bool
	ok   bool
}

func (g *gen) sortFile(name string) (*ast.File, bool) {
	sp := g.pkg.Imports["sort"]
	if sp == nil {
		return nil, false
	}
	for _, f := range sp.GoFiles {
		if filepath.Base(f) == name {
			src, err := os.ReadFile(f)
			if err != nil {
				return nil, false
			}
			af, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
			if err != nil {
				return nil, false
			}
			return af, true
		}
	}
	return nil, false
}

// emitSortTemplates adds the generic algorithms to the generated file.
func (g *gen) emitSortTemplates() bool {
	if g.sortT == nil {
		g.sortT = &sortTemplates{}
	}
	t := g.sortT
	if t.done {
		return t.ok
	}
	t.done = true
	helpers, ok1 := g.sortFile("sort.go")
	zfunc, ok2 := g.sortFile("zsortfunc.go")
	ziface, ok3 := g.sortFile("zsortinterface.go")
	if !ok1 || !ok2 || !ok3 {
		return false
	}
	ren := map[string]string{
		"sortedHint": "marmot_sortedHint", "unknownHint": "marmot_unknownHint",
		"increasingHint": "marmot_increasingHint", "decreasingHint": "marmot_decreasingHint",
		"xorshift": "marmot_xorshift", "nextPowerOfTwo": "marmot_nextPowerOfTwo",
	}
	var out []string
	// Helpers from sort.go.
	for _, d := range helpers.Decls {
		keep := false
		switch d := d.(type) {
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					keep = keep || s.Name.Name == "sortedHint" || s.Name.Name == "xorshift"
				case *ast.ValueSpec:
					for _, n := range s.Names {
						keep = keep || n.Name == "unknownHint"
					}
				}
			}
		case *ast.FuncDecl:
			keep = d.Name.Name == "nextPowerOfTwo" || (d.Recv != nil && d.Name.Name == "Next")
		}
		if keep {
			out = append(out, renameDecl(d, ren, nil, ""))
		}
	}
	// zsortfunc.go: data lessSwap -> data marmot_sortdata[E, L].
	fren := map[string]string{"lessSwap": "marmot_sortdata[E, L]"}
	maps.Copy(fren, ren)
	for _, d := range zfunc.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			fren[fd.Name.Name] = fd.Name.Name + "_m"
		}
	}
	for _, d := range zfunc.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			out = append(out, renameDecl(fd, fren, []string{"E any", "L marmot_lesser"}, fd.Name.Name+"_m"))
		}
	}
	// zsortinterface.go: data Interface -> data D.
	iren := map[string]string{"Interface": "D"}
	maps.Copy(iren, ren)
	for _, d := range ziface.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			iren[fd.Name.Name] = fd.Name.Name + "_mi"
		}
	}
	for _, d := range ziface.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			out = append(out, renameDecl(fd, iren, []string{"D marmot_iface"}, fd.Name.Name+"_mi"))
		}
	}
	out = append(out, `type marmot_lesser interface{ Less(i, j int) bool }

type marmot_iface interface {
	Len() int
	Less(i, j int) bool
	Swap(i, j int)
}

type marmot_sortdata[E any, L marmot_lesser] struct {
	s []E
	l L
}

func (d marmot_sortdata[E, L]) Less(i, j int) bool { return d.l.Less(i, j) }

func (d marmot_sortdata[E, L]) Swap(i, j int) { d.s[i], d.s[j] = d.s[j], d.s[i] }

type marmot_lessfn func(i, j int) bool

func (f marmot_lessfn) Less(i, j int) bool { return f(i, j) }

func marmot_sortSlice[E any, L marmot_lesser](x []E, l L) {
	length := len(x)
	limit := bits.Len(uint(length))
	pdqsort_func_m(marmot_sortdata[E, L]{x, l}, 0, length, limit)
}

func marmot_sortSliceStable[E any, L marmot_lesser](x []E, l L) {
	stable_func_m(marmot_sortdata[E, L]{x, l}, len(x))
}

func marmot_sortSort[D marmot_iface](data D) {
	n := data.Len()
	if n <= 1 {
		return
	}
	limit := bits.Len(uint(n))
	pdqsort_mi(data, 0, n, limit)
}

func marmot_sortStable[D marmot_iface](data D) {
	stable_mi(data, data.Len())
}
`)
	g.decls = append(g.decls, strings.Join(out, "\n"))
	g.usePkg("math/bits", "bits")
	t.ok = true
	return true
}

// renameDecl prints d with identifiers renamed by ren. With tparams, a
// function gets those type parameters and the new name.
func renameDecl(d ast.Decl, ren map[string]string, tparams []string, newName string) string {
	ast.Inspect(d, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if r, ok := ren[id.Name]; ok {
				id.Name = r
			}
		}
		return true
	})
	if fd, ok := d.(*ast.FuncDecl); ok {
		fd.Doc = nil
		if tparams != nil {
			fd.Name.Name = newName + "[" + strings.Join(tparams, ", ") + "]"
		}
	}
	if gd, ok := d.(*ast.GenDecl); ok {
		gd.Doc = nil
	}
	var b bytes.Buffer
	printer.Fprint(&b, token.NewFileSet(), d)
	return b.String() + "\n"
}

func rewriteSort(g *gen, f *ast.File) {
	if !g.on["sort"] {
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn := calleeFunc(g.info, call)
		if fn == nil || fn.Pkg().Path() != "sort" || call.Ellipsis.IsValid() {
			return true
		}
		switch fn.Name() {
		case "Slice", "SliceStable":
			if len(call.Args) == 2 {
				g.sortSlice(call, fn.Name() == "SliceStable")
			}
		case "Sort", "Stable":
			if len(call.Args) == 1 {
				g.sortSort(call, fn.Name() == "Stable")
			}
		}
		return true
	})
}

func (g *gen) sortSort(call *ast.CallExpr, stable bool) {
	t := g.info.Types[call.Args[0]].Type
	if t == nil || types.IsInterface(t) {
		return
	}
	if _, isTP := t.(*types.TypeParam); isTP {
		return
	}
	if !g.emitSortTemplates() {
		return
	}
	name := "marmot_sortSort"
	if stable {
		name = "marmot_sortStable"
	}
	if g.oldFile() {
		// No generic calls in a file before go1.18: call a plain wrapper.
		ts := g.typeExpr(t)
		if ts == "" {
			return
		}
		w := g.newName("sortw")
		g.decls = append(g.decls, fmt.Sprintf("func %s(d %s) { %s(d) }\n", w, ts, name))
		name = w
	}
	g.replaceCall(call, name, []ast.Expr{call.Args[0]})
}

func (g *gen) sortSlice(call *ast.CallExpr, stable bool) {
	t := g.info.Types[call.Args[0]].Type
	if t == nil {
		return
	}
	if _, isTP := t.(*types.TypeParam); isTP {
		return
	}
	if _, ok := t.Underlying().(*types.Slice); !ok {
		return
	}
	less := call.Args[1]
	if tv := g.info.Types[less]; tv.IsNil() {
		return // sort.Slice panics calling a nil less; keep that path
	}
	if !g.emitSortTemplates() {
		return
	}
	lessArg, ok := g.lessStruct(less)
	if !ok {
		lessArg = &ast.CallExpr{Fun: ast.NewIdent("marmot_lessfn"), Args: []ast.Expr{less}}
	}
	name := "marmot_sortSlice"
	if stable {
		name = "marmot_sortSliceStable"
	}
	if g.oldFile() {
		ts := g.typeExpr(t)
		lt := "marmot_lessfn"
		if cl, ok := lessArg.(*ast.CompositeLit); ok {
			lt = cl.Type.(*ast.Ident).Name
		}
		if ts == "" {
			return
		}
		w := g.newName("sortw")
		g.decls = append(g.decls, fmt.Sprintf("func %s(x %s, l %s) { %s(x, l) }\n", w, ts, lt, name))
		name = w
	}
	g.replaceCall(call, name, []ast.Expr{call.Args[0], lessArg})
}

// lessStruct turns a less function literal into a value of a generated
// struct type whose Less method is the literal's body, with each captured
// variable reached through a pointer field. It fails when the literal
// cannot move to package level unchanged.
func (g *gen) lessStruct(e ast.Expr) (ast.Expr, bool) { return g.closureStruct(e, "Less") }

// closureStruct turns a function literal into a value of a generated
// struct type whose method (named method) is the literal's body, with each
// captured variable reached through a pointer field.
func (g *gen) closureStruct(e ast.Expr, method string) (ast.Expr, bool) {
	lit, ok := ast.Unparen(e).(*ast.FuncLit)
	if !ok {
		return nil, false
	}
	type free struct {
		obj   *types.Var
		field string
	}
	var frees []*free
	byObj := map[*types.Var]*free{}
	var idents []*ast.Ident
	pkgNames := map[string]string{} // local name -> path
	bad := false
	pkgScope := g.pkg.Types.Scope()
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			bad = true
		case *ast.CallExpr:
			if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok {
				if b, ok := g.info.Uses[id].(*types.Builtin); ok && b.Name() == "recover" {
					bad = true
				}
			}
		case *ast.Ident:
			switch obj := g.info.Uses[n].(type) {
			case *types.PkgName:
				pkgNames[n.Name] = obj.Imported().Path()
			case *types.Var:
				if obj.IsField() || obj.Parent() == pkgScope || obj.Parent() == nil {
					return true
				}
				if obj.Pos() >= lit.Pos() && obj.Pos() < lit.End() {
					return true // declared inside the literal
				}
				fv := byObj[obj]
				if fv == nil {
					fv = &free{obj: obj, field: fmt.Sprintf("f%d", len(frees))}
					byObj[obj] = fv
					frees = append(frees, fv)
				}
				idents = append(idents, n)
			case *types.TypeName:
				if obj.Parent() != pkgScope && obj.Parent() != types.Universe {
					bad = true // local type
				}
			case *types.Const:
				if obj.Parent() != pkgScope && obj.Parent() != types.Universe {
					bad = true // local constant
				}
			}
		}
		return !bad
	})
	if bad {
		return nil, false
	}
	var fields []string
	for _, fv := range frees {
		ts := g.typeExpr(fv.obj.Type())
		if ts == "" {
			return nil, false
		}
		fields = append(fields, fmt.Sprintf("%s *%s", fv.field, ts))
	}
	// Print the body with captured variables reached through the receiver.
	saved := make([]string, len(idents))
	for i, id := range idents {
		saved[i] = id.Name
		id.Name = "(*marmot_l." + byObj[g.info.Uses[id].(*types.Var)].field + ")"
	}
	var body, sig bytes.Buffer
	err1 := printer.Fprint(&body, g.pkg.Fset, lit.Body)
	err2 := printer.Fprint(&sig, g.pkg.Fset, &ast.FuncType{Params: lit.Type.Params, Results: lit.Type.Results})
	for i, id := range idents {
		id.Name = saved[i]
	}
	if err1 != nil || err2 != nil || !strings.HasPrefix(sig.String(), "func(") {
		return nil, false
	}
	for name, path := range pkgNames {
		if std, ok := tokenPaths[name]; ok && std != path {
			return nil, false // the generated code reads name. as the standard package
		}
		if !g.importAs(path, name) {
			return nil, false
		}
	}
	params := strings.TrimPrefix(sig.String(), "func")
	tname := g.newName("closure")
	g.decls = append(g.decls, fmt.Sprintf("type %s struct {\n\t%s\n}\n\nfunc (marmot_l %s) %s%s %s\n",
		tname, strings.Join(fields, "\n\t"), tname, method, params, body.String()))
	lt := &ast.CompositeLit{Type: ast.NewIdent(tname)}
	for _, fv := range frees {
		lt.Elts = append(lt.Elts, &ast.UnaryExpr{Op: token.AND, X: ast.NewIdent(fv.obj.Name())})
	}
	// The new &v expressions must resolve to the same variables: the
	// identifiers are re-type-checked in place of the literal, where the
	// literal's free variables are in scope under the same names.
	return lt, true
}

// oldFile reports whether the file being rewritten is compiled at a
// language version without generics (its module declares go < 1.18).
func (g *gen) oldFile() bool {
	v := g.info.FileVersions[g.curFile]
	return v != "" && version.Compare(v, "go1.18") < 0
}
