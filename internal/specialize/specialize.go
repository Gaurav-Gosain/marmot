// Package specialize rewrites standard library calls in the program being
// compiled into code specialized for what the compiler knows at the call
// site: constant arguments and static types. See docs/specialize.md.
//
// Every rewrite is an optimization, never a semantic change. A rewriter that
// is unsure leaves the call alone.
package specialize

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// Input is what the driver hands the pass.
type Input struct {
	Packages []*packages.Package // every package, type checked, with syntax, dependencies first
	Prog     *ssa.Program        // nil in BeforeSSA
	Pkgs     []*ssa.Package
	Release  bool
	GOOS     string
	GOARCH   string
	Log      func(format string, args ...any) // optional
}

// GenFile is the base name of the file the pass adds to a rewritten package.
const GenFile = "zz_marmot_spec.go"

// Enabled reports whether the pass runs: in release builds, or when
// MARMOT_SPEC=1; never when MARMOT_SPEC=0.
func Enabled(release bool) bool {
	switch os.Getenv("MARMOT_SPEC") {
	case "0":
		return false
	case "1":
		return true
	}
	return release
}

// BeforeSSA rewrites typed syntax of user packages. On return every changed
// package (and every user package importing one) has fresh Syntax, Types and
// TypesInfo.
func BeforeSSA(in *Input) error {
	if !Enabled(in.Release) {
		return nil
	}
	res, err := Rewrite(in.Packages)
	if err != nil {
		return err
	}
	if in.Log != nil && res.Sites > 0 {
		in.Log("specialize: %d call sites in %d packages", res.Sites, len(res.Changed))
	}
	if dir := os.Getenv("MARMOT_SPEC_DUMP"); dir != "" {
		if err := res.Dump(dir); err != nil {
			return err
		}
	}
	return nil
}

// AfterSSA may replace function bodies after SSA is built. Nothing yet.
func AfterSSA(in *Input) error { return nil }

// Result describes what Rewrite changed.
type Result struct {
	Sites   int
	Changed []*packages.Package
	Gen     map[*packages.Package]string // generated file source per changed package
}

// Dump writes every changed file and generated file under dir, one
// directory per package path.
func (r *Result) Dump(dir string) error {
	for _, p := range r.Changed {
		d := filepath.Join(dir, filepath.FromSlash(p.PkgPath))
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
		for _, f := range p.Syntax {
			name := filepath.Base(p.Fset.File(f.Pos()).Name())
			if name == GenFile {
				continue
			}
			var buf bytes.Buffer
			if err := format.Node(&buf, p.Fset, f); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(d, name), buf.Bytes(), 0o644); err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(d, GenFile), []byte(r.Gen[p]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// isUser reports whether p is program code rather than the standard library.
func isUser(p *packages.Package) bool {
	return p.Module != nil || p.PkgPath == "command-line-arguments" || p.Name == "main"
}

// Rewrite runs every rewriter over the user packages of pkgs (dependencies
// first) and type checks what changed.
func Rewrite(pkgs []*packages.Package) (*Result, error) {
	res := &Result{Gen: map[*packages.Package]string{}}
	byPath := map[string]*packages.Package{}
	for _, p := range pkgs {
		byPath[p.PkgPath] = p
	}
	rechecked := map[string]bool{}
	for _, p := range pkgs {
		if !isUser(p) || p.Types == nil || p.TypesInfo == nil || len(p.Syntax) == 0 {
			continue
		}
		g := newGen(p)
		for _, f := range p.Syntax {
			before := g.sites
			rewriteFmt(g, f)
			rewriteJSON(g, f)
			if g.sites > before {
				g.blankUnusedImports(f)
			}
		}
		depChanged := false
		for path := range p.Imports {
			if rechecked[path] {
				depChanged = true
			}
		}
		if g.sites == 0 && !depChanged {
			continue
		}
		src := ""
		if g.sites > 0 {
			src = g.source()
		}
		if err := recheck(p, byPath, src); err != nil {
			g.undo()
			if g.sites > 0 && depChanged {
				// The package must be checked again against the new
				// dependencies even without its own rewrites.
				if err2 := recheck(p, byPath, ""); err2 != nil {
					return nil, fmt.Errorf("specialize: %s: %v", p.PkgPath, err2)
				}
				rechecked[p.PkgPath] = true
			} else if depChanged {
				return nil, fmt.Errorf("specialize: %s: %v", p.PkgPath, err)
			}
			if os.Getenv("MARMOT_SPEC_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "specialize: %s: rewrite dropped: %v\n%s\n", p.PkgPath, err, src)
			}
			continue
		}
		rechecked[p.PkgPath] = true
		if g.sites > 0 {
			res.Sites += g.sites
			res.Changed = append(res.Changed, p)
			res.Gen[p] = src
		}
	}
	return res, nil
}

// recheck type checks p again from its (rewritten) syntax plus the generated
// source, against the current Types of its imports, and installs the result.
func recheck(p *packages.Package, byPath map[string]*packages.Package, gen string) error {
	files := make([]*ast.File, 0, len(p.Syntax)+1)
	for _, f := range p.Syntax {
		if filepath.Base(p.Fset.File(f.Pos()).Name()) != GenFile {
			files = append(files, f)
		}
	}
	if gen != "" {
		dir := "."
		if len(p.CompiledGoFiles) > 0 {
			dir = filepath.Dir(p.CompiledGoFiles[0])
		}
		f, err := parser.ParseFile(p.Fset, filepath.Join(dir, GenFile), gen, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("generated code does not parse: %v", err)
		}
		files = append(files, f)
	}
	info := &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Instances:    map[*ast.Ident]types.Instance{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		FileVersions: map[*ast.File]string{},
	}
	var errs []string
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			if path == "unsafe" {
				return types.Unsafe, nil
			}
			if ip, ok := p.Imports[path]; ok && ip.Types != nil {
				return ip.Types, nil
			}
			if ip, ok := byPath[path]; ok && ip.Types != nil {
				return ip.Types, nil
			}
			return nil, fmt.Errorf("package %q not loaded", path)
		}),
		Sizes: p.TypesSizes,
		Error: func(err error) {
			if len(errs) < 10 {
				errs = append(errs, err.Error())
			}
		},
	}
	if p.Module != nil && p.Module.GoVersion != "" {
		conf.GoVersion = "go" + p.Module.GoVersion
	}
	tp, _ := conf.Check(p.PkgPath, p.Fset, files, info)
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	tp.SetImports(sortedImports(tp.Imports()))
	p.Syntax = files
	p.Types = tp
	p.TypesInfo = info
	return nil
}

func sortedImports(ps []*types.Package) []*types.Package {
	sort.Slice(ps, func(i, j int) bool { return ps[i].Path() < ps[j].Path() })
	return ps
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// gen collects the generated declarations for one package.
type gen struct {
	pkg     *packages.Package
	info    *types.Info
	sites   int
	n       int
	imports map[string]string // path -> local name in the generated file
	decls   []string
	undos   []func()
	tokens  map[string]string // short package name used in decls -> import path
	json    *jsonEnc
	jdec    *jsonDec
}

func newGen(p *packages.Package) *gen {
	return &gen{pkg: p, info: p.TypesInfo, imports: map[string]string{}}
}

// importName returns the name under which the generated file refers to path.
func (g *gen) importName(path string) string {
	if n, ok := g.imports[path]; ok {
		return n
	}
	n := fmt.Sprintf("mspec_%d", len(g.imports))
	g.imports[path] = n
	return n
}

// typeExpr returns source for t as seen from the generated file, or "" when
// t cannot be named there (unexported foreign types, type parameters).
func (g *gen) typeExpr(t types.Type) string {
	ok := true
	var check func(t types.Type)
	seen := map[types.Type]bool{}
	check = func(t types.Type) {
		if seen[t] {
			return
		}
		seen[t] = true
		switch t := t.(type) {
		case *types.TypeParam:
			ok = false
		case *types.Alias:
			check(types.Unalias(t))
		case *types.Named:
			obj := t.Obj()
			if obj.Pkg() != nil && obj.Pkg() != g.pkg.Types && !obj.Exported() {
				ok = false
			}
			if obj.Parent() != nil && obj.Pkg() != nil && obj.Parent() != obj.Pkg().Scope() {
				ok = false // local type
			}
			if args := t.TypeArgs(); args != nil {
				for i := 0; i < args.Len(); i++ {
					check(args.At(i))
				}
			}
		case *types.Pointer:
			check(t.Elem())
		case *types.Slice:
			check(t.Elem())
		case *types.Array:
			check(t.Elem())
		case *types.Map:
			check(t.Key())
			check(t.Elem())
		case *types.Chan:
			check(t.Elem())
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				f := t.Field(i)
				if !f.Exported() && f.Pkg() != g.pkg.Types {
					ok = false
				}
				check(f.Type())
			}
		case *types.Signature:
			for i := 0; i < t.Params().Len(); i++ {
				check(t.Params().At(i).Type())
			}
			for i := 0; i < t.Results().Len(); i++ {
				check(t.Results().At(i).Type())
			}
		case *types.Interface:
			for i := 0; i < t.NumMethods(); i++ {
				m := t.Method(i)
				if !m.Exported() && m.Pkg() != g.pkg.Types {
					ok = false
				}
				check(m.Type())
			}
			for i := 0; i < t.NumEmbeddeds(); i++ {
				check(t.EmbeddedType(i))
			}
		case *types.Tuple, *types.Union:
			ok = false
		}
	}
	check(t)
	if !ok {
		return ""
	}
	return types.TypeString(t, func(p *types.Package) string {
		if p == g.pkg.Types {
			return ""
		}
		return g.importName(p.Path())
	})
}

// usePkg lets generated declarations refer to package path as name;
// source() rewrites name. to the import alias.
func (g *gen) usePkg(path, name string) {
	if g.tokens == nil {
		g.tokens = map[string]string{}
	}
	g.tokens[name] = path
}

func (g *gen) newName(kind string) string {
	g.n++
	return fmt.Sprintf("marmot_spec_%s_%d", kind, g.n)
}

func (g *gen) undo() {
	for i := len(g.undos) - 1; i >= 0; i-- {
		g.undos[i]()
	}
	g.undos = nil
}

// replaceCall points call at the generated function name with new args.
func (g *gen) replaceCall(call *ast.CallExpr, name string, args []ast.Expr) {
	oldFun, oldArgs, oldEll := call.Fun, call.Args, call.Ellipsis
	g.undos = append(g.undos, func() { call.Fun, call.Args, call.Ellipsis = oldFun, oldArgs, oldEll })
	call.Fun = &ast.Ident{Name: name, NamePos: call.Fun.Pos()}
	call.Args = args
	call.Ellipsis = token.NoPos
	g.sites++
}

// blankUnusedImports turns imports that no identifier of f uses any more
// (after rewriting) into blank imports, which keeps their initialization.
func (g *gen) blankUnusedImports(f *ast.File) {
	used := map[types.Object]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if pn, ok := g.info.Uses[id].(*types.PkgName); ok {
				used[pn] = true
			}
		}
		return true
	})
	for _, spec := range f.Imports {
		var obj types.Object
		if spec.Name != nil {
			obj = g.info.Defs[spec.Name]
		} else {
			obj = g.info.Implicits[spec]
		}
		pn, ok := obj.(*types.PkgName)
		if !ok || used[pn] {
			continue
		}
		spec := spec
		old := spec.Name
		g.undos = append(g.undos, func() { spec.Name = old })
		spec.Name = &ast.Ident{Name: "_", NamePos: spec.Pos()}
	}
}

func (g *gen) source() string {
	g.resolveTokens()
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by marmot internal/specialize. DO NOT EDIT.\n\npackage %s\n\n", g.pkg.Types.Name())
	paths := make([]string, 0, len(g.imports))
	for p := range g.imports {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) > 0 {
		b.WriteString("import (\n")
		for _, p := range paths {
			fmt.Fprintf(&b, "\t%s %q\n", g.imports[p], p)
		}
		b.WriteString(")\n\n")
	}
	for _, d := range g.decls {
		b.WriteString(d)
		b.WriteString("\n")
	}
	return b.String()
}

var pkgTokenRE = regexp.MustCompile(`\b(json|strconv|slices)\.`)

// resolveTokens replaces the short package names registered with usePkg.
func (g *gen) resolveTokens() {
	if len(g.tokens) == 0 {
		return
	}
	for i, d := range g.decls {
		g.decls[i] = pkgTokenRE.ReplaceAllStringFunc(d, func(m string) string {
			path, ok := g.tokens[m[:len(m)-1]]
			if !ok {
				return m
			}
			return g.importName(path) + "."
		})
	}
}

// calleeFunc returns the package level function a call statically targets.
func calleeFunc(info *types.Info, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
		if sel, ok := info.Selections[f]; ok && sel != nil {
			return nil // method value or method call
		}
	default:
		return nil
	}
	fn, _ := info.Uses[id].(*types.Func)
	if fn == nil || fn.Pkg() == nil {
		return nil
	}
	if sig, ok := fn.Type().(*types.Signature); !ok || sig.Recv() != nil {
		return nil
	}
	return fn
}
