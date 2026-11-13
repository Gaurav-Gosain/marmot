package specialize

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Cloning standard functions that take a func argument.
//
// sort.Search(n, func(i int) bool {...}), slices.IndexFunc(s, f) and the
// like call their func argument through a func value. The clone is the
// function's own source from the loaded GOROOT, made generic over the type
// of that argument (a method set with one method, Call), so a function
// literal at the call site becomes a struct whose Call method the backend
// can inline. References in the body to the package's exported names are
// qualified; a body that uses an unexported name of its package, or uses
// the func parameter other than by calling it, is not cloned. The clone
// computes what the original computes: same statements, same order.

// cloneTargets are the functions cloned, with the index of the func
// parameter.
var cloneTargets = map[string]map[string]int{
	"sort":    {"Search": 1},
	"slices":  {"IndexFunc": 1, "ContainsFunc": 1},
	"strings": {"Map": 0, "IndexFunc": 1, "ContainsFunc": 1},
}

func rewriteClone(g *gen, f *ast.File) {
	if !g.on["clone"] {
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Ellipsis.IsValid() {
			return true
		}
		fn := calleeFunc(g.info, call)
		if fn == nil {
			return true
		}
		idx, ok := cloneTargets[fn.Pkg().Path()][fn.Name()]
		if !ok || idx >= len(call.Args) {
			return true
		}
		if tv := g.info.Types[call.Args[idx]]; tv.IsNil() {
			return true
		}
		g.cloneCall(call, fn, idx)
		return true
	})
}

// cloneCall replaces call by a call of the clone of fn.
func (g *gen) cloneCall(call *ast.CallExpr, fn *types.Func, idx int) {
	name, ok := g.cloneOf(fn, idx)
	if !ok {
		return
	}
	arg, ok := g.closureStruct(call.Args[idx], "Call")
	if !ok {
		w, ok := g.funcWrapper(g.info.Types[call.Args[idx]].Type)
		if !ok {
			return
		}
		arg = &ast.CallExpr{Fun: ast.NewIdent(w), Args: []ast.Expr{call.Args[idx]}}
	}
	args := append([]ast.Expr{}, call.Args...)
	args[idx] = arg
	g.replaceCall(call, name, args)
}

// funcWrapper returns a named func type with a Call method for func type t.
func (g *gen) funcWrapper(t types.Type) (string, bool) {
	sig, ok := t.Underlying().(*types.Signature)
	if !ok {
		return "", false
	}
	src := g.typeExpr(sig)
	if src == "" {
		return "", false
	}
	if g.wrappers == nil {
		g.wrappers = map[string]string{}
	}
	if w, ok := g.wrappers[src]; ok {
		return w, true
	}
	w := g.newName("fnw")
	var ps, as []string
	for i := 0; i < sig.Params().Len(); i++ {
		ps = append(ps, fmt.Sprintf("p%d %s", i, g.typeExpr(sig.Params().At(i).Type())))
		as = append(as, fmt.Sprintf("p%d", i))
	}
	var rs []string
	for v := range sig.Results().Variables() {
		rt := g.typeExpr(v.Type())
		if rt == "" {
			return "", false
		}
		rs = append(rs, rt)
	}
	res := ""
	switch len(rs) {
	case 0:
	case 1:
		res = " " + rs[0]
	default:
		res = " (" + strings.Join(rs, ", ") + ")"
	}
	if sig.Variadic() {
		return "", false
	}
	ret := "return "
	if sig.Results().Len() == 0 {
		ret = ""
	}
	g.decls = append(g.decls, fmt.Sprintf("type %s %s\n\nfunc (f %s) Call(%s)%s { %sf(%s) }\n",
		w, src, w, strings.Join(ps, ", "), res, ret, strings.Join(as, ", ")))
	g.wrappers[src] = w
	return w, true
}

// cloneOf generates (once) the clone of fn generic over its func
// parameter idx and returns its name.
func (g *gen) cloneOf(fn *types.Func, idx int) (string, bool) {
	key := fn.FullName()
	if g.clones == nil {
		g.clones = map[string]string{}
	}
	if name, ok := g.clones[key]; ok {
		return name, name != ""
	}
	g.clones[key] = ""
	var sp *packages.Package
	for _, p := range g.allPkgs {
		if p.Types == fn.Pkg() {
			sp = p
		}
	}
	if sp == nil || sp.TypesInfo == nil {
		return "", false
	}
	var decl *ast.FuncDecl
	for _, f := range sp.Syntax {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == fn.Name() && fd.Body != nil {
				decl = fd
			}
		}
	}
	if decl == nil {
		return "", false
	}
	info := sp.TypesInfo
	pvar := fn.Type().(*types.Signature).Params().At(idx)
	pkgScope := sp.Types.Scope()
	alias := g.importName(sp.PkgPath)
	// Check the body and collect renames: exported package names get the
	// alias, calls of the func parameter become .Call.
	type ren struct {
		id   *ast.Ident
		name string
	}
	var rens []ren
	bad := false
	callees := map[*ast.Ident]bool{}
	ast.Inspect(decl, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if id, ok := ast.Unparen(c.Fun).(*ast.Ident); ok && info.Uses[id] == pvar {
				callees[id] = true
			}
		}
		return true
	})
	ast.Inspect(decl, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || bad {
			return !bad
		}
		obj := info.Uses[id]
		switch {
		case obj == nil:
		case obj == pvar:
			if !callees[id] {
				bad = true // the func value escapes or is compared
			} else {
				rens = append(rens, ren{id, id.Name + ".Call"})
			}
		case obj.Pkg() == sp.Types && obj.Parent() == pkgScope:
			if !obj.Exported() {
				bad = true
			} else {
				rens = append(rens, ren{id, alias + "." + id.Name})
			}
		case obj.Pkg() != nil && obj.Pkg() != sp.Types && obj.Parent() == obj.Pkg().Scope():
			// Another package's name, written pkg.Name in the source:
			// the selector's package identifier is handled below.
		}
		if pn, ok := obj.(*types.PkgName); ok {
			rens = append(rens, ren{id, g.importName(pn.Imported().Path())})
		}
		return true
	})
	if bad {
		return "", false
	}
	name := g.newName("clone")
	iface := name + "_fn"
	// Rewrite: rename, replace the parameter type, print, restore.
	saved := make([]string, len(rens))
	for i, r := range rens {
		saved[i], r.id.Name = r.id.Name, r.name
	}
	oldName := decl.Name.Name
	var tparams []string
	var tnames []string
	if decl.Type.TypeParams != nil {
		var b bytes.Buffer
		printer.Fprint(&b, token.NewFileSet(), &ast.IndexListExpr{X: ast.NewIdent(""), Indices: nil})
		for _, fl := range decl.Type.TypeParams.List {
			var tb bytes.Buffer
			printer.Fprint(&tb, token.NewFileSet(), fl.Type)
			var ns []string
			for _, n := range fl.Names {
				ns = append(ns, n.Name)
				tnames = append(tnames, n.Name)
			}
			tparams = append(tparams, strings.Join(ns, ", ")+" "+tb.String())
		}
	}
	ifaceRef := iface
	if len(tnames) > 0 {
		ifaceRef = iface + "[" + strings.Join(tnames, ", ") + "]"
	}
	tparamsOrig := append([]string{}, tparams...)
	tparams = append(tparams, "Fn "+ifaceRef)
	field := decl.Type.Params.List[0]
	var ptypeExpr ast.Expr
	// Find the field holding parameter idx and swap its type.
	i := 0
	for _, fl := range decl.Type.Params.List {
		n := len(fl.Names)
		if n == 0 {
			n = 1
		}
		if idx < i+n {
			field = fl
			break
		}
		i += n
	}
	if len(field.Names) != 1 {
		for j, r := range rens {
			r.id.Name = saved[j]
		}
		return "", false
	}
	var ftb bytes.Buffer
	printer.Fprint(&ftb, token.NewFileSet(), field.Type)
	callSig := strings.TrimPrefix(ftb.String(), "func")
	ptypeExpr, field.Type = field.Type, ast.NewIdent("Fn")
	decl.Name.Name = name + "[" + strings.Join(tparams, ", ") + "]"
	savedTP := decl.Type.TypeParams
	decl.Type.TypeParams = nil
	savedDoc := decl.Doc
	decl.Doc = nil
	var out bytes.Buffer
	err := printer.Fprint(&out, sp.Fset, decl)
	decl.Doc = savedDoc
	decl.Type.TypeParams = savedTP
	decl.Name.Name = oldName
	field.Type = ptypeExpr
	for j, r := range rens {
		r.id.Name = saved[j]
	}
	if err != nil {
		return "", false
	}
	ifaceDecl := iface
	if len(tparamsOrig) > 0 {
		ifaceDecl = iface + "[" + strings.Join(tparamsOrig, ", ") + "]"
	}
	g.decls = append(g.decls, fmt.Sprintf("type %s interface{ Call%s }\n\n%s\n", ifaceDecl, callSig, out.String()))
	g.clones[key] = name
	return name, true
}
