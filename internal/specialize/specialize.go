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
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
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

// Features are the specializations, each behind its own switch.
var Features = []string{"fmt", "jsonenc", "jsondec", "sort", "regexp", "mapkey", "split", "builder", "fold"}

// Measured and kept off (enable by name only): clone (sort.Search,
// slices.IndexFunc, strings.Map clones were 9% slower than -release
// without them, which already inlines constant closures).
var optInFeatures = []string{"clone"}

// releaseDefault lists the features on by default in release builds. A
// feature joins it only after the full conformance matrix and a fuzz round
// pass with it forced on (MARMOT_SPEC=name); see docs/specialize.md.
var releaseDefault = map[string]bool{}

// enabledSet returns the features to run. MARMOT_SPEC selects them: "0"
// none, "1" or "all" every feature, or a comma list of names. Unset, release
// builds get releaseDefault and dev builds none.
func enabledSet(release bool) map[string]bool {
	v, set := os.LookupEnv("MARMOT_SPEC")
	on := map[string]bool{}
	switch {
	case !set || v == "":
		if release {
			for k := range releaseDefault {
				on[k] = true
			}
		}
	case v == "0":
	case v == "1" || v == "all":
		for _, k := range Features {
			on[k] = true
		}
	default:
		for _, k := range strings.Split(v, ",") {
			on[strings.TrimSpace(k)] = true
		}
	}
	return on
}

// Enabled reports whether any feature runs.
func Enabled(release bool) bool { return len(enabledSet(release)) > 0 }

// BeforeSSA rewrites typed syntax of user packages. On return every changed
// package (and every user package importing one) has fresh Syntax, Types and
// TypesInfo.
func BeforeSSA(in *Input) error {
	if !Enabled(in.Release) {
		return nil
	}
	res, err := RewriteWith(in.Packages, enabledSet(in.Release))
	if err != nil {
		return err
	}
	if in.Log != nil && len(res.Dropped) > 0 {
		in.Log("specialize: rewrite dropped in %s (MARMOT_SPEC_DEBUG=1 shows why)", strings.Join(res.Dropped, ", "))
	}
	if in.Log != nil && res.Sites > 0 {
		var ks []string
		for k, n := range res.Kinds {
			ks = append(ks, fmt.Sprintf("%s %d", k, n))
		}
		sort.Strings(ks)
		in.Log("specialize: %d call sites in %d packages (%s)", res.Sites, len(res.Changed), strings.Join(ks, ", "))
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
	Dropped []string       // packages whose rewrite failed to type check (left as written)
	Skips   map[string]int // sites left alone, by reason
	Kinds   map[string]int // sites per feature
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
	on := map[string]bool{}
	for _, k := range Features {
		on[k] = true
	}
	return RewriteWith(pkgs, on)
}

// RewriteWith is Rewrite with only the features in on.
func RewriteWith(pkgs []*packages.Package, on map[string]bool) (*Result, error) {
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
		g.on = on
		g.allPkgs = pkgs
		for _, f := range p.Syntax {
			before := g.sites
			g.curFile = f
			for _, r := range []struct {
				name string
				run  func(*gen, *ast.File)
			}{{"fmt", rewriteFmt}, {"json", rewriteJSON}, {"sort", rewriteSort}, {"regexp", rewriteRegexp},
				{"mapkey", rewriteMapKey}, {"split", rewriteSplit}, {"builder", rewriteBuilder},
				{"clone", rewriteClone}, {"fold", rewriteFold}} {
				g.feature = r.name
				r.run(g, f)
			}
			if g.sites > before {
				g.blankUnusedImports(f)
			}
		}
		if len(g.skips) > 0 {
			if res.Skips == nil {
				res.Skips = map[string]int{}
			}
			for k, n := range g.skips {
				res.Skips[k] += n
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
			res.Dropped = append(res.Dropped, p.PkgPath)
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
			if res.Kinds == nil {
				res.Kinds = map[string]int{}
			}
			for k, n := range g.kinds {
				res.Kinds[k] += n
			}
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
	pkg          *packages.Package
	info         *types.Info
	sites        int
	n            int
	imports      map[string]string // path -> local name in the generated file
	decls        []string
	undos        []func()
	tokens       map[string]string // short package name used in decls -> import path
	json         *jsonEnc
	jdec         *jsonDec
	on           map[string]bool // enabled features
	sortT        *sortTemplates
	named        map[string]string // import name -> path, for code moved from user files
	raw          []string          // declarations not subject to usePkg rewriting
	allPkgs      []*packages.Package
	needMapBytes bool
	curFile      *ast.File
	clones       map[string]string
	wrappers     map[string]string
	feature      string         // rewriter running now
	kinds        map[string]int // sites per feature
	skips        map[string]int // sites left alone, by reason
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

// importAs makes the generated file import path under name, as a user
// file does; it fails if name is taken by another path.
func (g *gen) importAs(path, name string) bool {
	if g.named == nil {
		g.named = map[string]string{}
	}
	if p, ok := g.named[name]; ok {
		return p == path
	}
	if strings.HasPrefix(name, "mspec_") {
		return false
	}
	g.named[name] = path
	return true
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
	g.count()
}

// blankUnusedImports turns imports that no identifier of f uses any more
// (after rewriting) into blank imports, which keeps their initialization.
func (g *gen) blankUnusedImports(f *ast.File) {
	used := map[types.Object]bool{}
	newNames := map[string]bool{} // identifiers the rewrites created (not type checked yet)
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if obj, known := g.info.Uses[id]; known {
				if pn, ok := obj.(*types.PkgName); ok {
					used[pn] = true
				}
			} else if _, def := g.info.Defs[id]; !def {
				newNames[id.Name] = true
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
		if !ok || used[pn] || newNames[pn.Name()] {
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
	body := strings.Join(g.decls, "\n") + strings.Join(g.raw, "\n")
	usedName := func(n string) bool { return usesQualifier(body, n) }
	for p, n := range g.imports {
		if !usedName(n) {
			delete(g.imports, p)
		}
	}
	for n := range g.named {
		if !usedName(n) {
			delete(g.named, n)
		}
	}
	var b strings.Builder
	// The build line sets this file's language version, so generic helpers
	// type check in modules that declare an older go version.
	fmt.Fprintf(&b, "// Code generated by marmot internal/specialize. DO NOT EDIT.\n\n//go:build go1.21\n\npackage %s\n\n", g.pkg.Types.Name())
	var specs []string
	for p, n := range g.imports {
		specs = append(specs, fmt.Sprintf("\t%s %q\n", n, p))
	}
	for n, p := range g.named {
		specs = append(specs, fmt.Sprintf("\t%s %q\n", n, p))
	}
	sort.Strings(specs)
	if len(specs) > 0 {
		b.WriteString("import (\n")
		for _, s := range specs {
			b.WriteString(s)
		}
		b.WriteString(")\n\n")
	}
	for _, d := range g.decls {
		b.WriteString(d)
		b.WriteString("\n")
	}
	for _, d := range g.raw {
		b.WriteString(d)
		b.WriteString("\n")
	}
	if g.needMapBytes {
		b.WriteString("//go:linkname marmot_mapassign_faststr_bytes runtime.mapassign_faststr_bytes\nfunc marmot_mapassign_faststr_bytes(t, m unsafe.Pointer, b []byte) unsafe.Pointer\n")
	}
	return b.String()
}

// tokenPaths are the package names generated code may use unqualified by
// alias (rewritten by resolveTokens), with the packages they stand for.
var tokenPaths = map[string]string{
	"json": "encoding/json", "strconv": "strconv", "slices": "slices", "bits": "math/bits",
	"math": "math", "regexp": "regexp", "strings": "strings", "utf8": "unicode/utf8",
	"syntax": "regexp/syntax",
}

// resolveTokens replaces the short package names registered with usePkg.
func (g *gen) resolveTokens() {
	if len(g.tokens) == 0 {
		return
	}
	for i, d := range g.decls {
		g.decls[i] = rewriteQualifiers(d, func(name string) (string, bool) {
			path, ok := g.tokens[name]
			if !ok {
				return "", false
			}
			return g.importName(path), true
		})
	}
}

// rewriteQualifiers renames identifiers used as qualifiers (an identifier
// followed by a period) in Go source src, through f. String literals and
// comments are never touched: generated code carries user text, such as
// format strings, that may contain "fmt." or "json.".
func rewriteQualifiers(src string, f func(name string) (string, bool)) string {
	var sc scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	sc.Init(file, []byte(src), nil, 0)
	var b strings.Builder
	last := 0
	type tok struct {
		pos int
		t   token.Token
		lit string
	}
	var prev, prev2 tok
	prev.pos = -1
	for {
		pos, t, lit := sc.Scan()
		if t == token.EOF {
			break
		}
		off := file.Offset(pos)
		// A qualifier is an identifier followed by a period and not itself
		// selected (x.fmt.y names a field fmt).
		if t == token.PERIOD && prev.t == token.IDENT && prev.pos >= 0 && prev2.t != token.PERIOD {
			if repl, ok := f(prev.lit); ok {
				b.WriteString(src[last:prev.pos])
				b.WriteString(repl)
				last = prev.pos + len(prev.lit)
			}
		}
		prev2, prev = prev, tok{off, t, lit}
	}
	b.WriteString(src[last:])
	return b.String()
}

// usesQualifier reports whether src uses name as a qualifier (name.X),
// outside string literals and comments.
func usesQualifier(src, name string) bool {
	used := false
	rewriteQualifiers(src, func(n string) (string, bool) {
		if n == name {
			used = true
		}
		return "", false
	})
	return used
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

// replaceStmt replaces statement old with new in the current file, with
// undo.
func (g *gen) replaceStmt(old, new ast.Stmt) {
	f := g.curFile
	done := false
	astutil.Apply(f, nil, func(c *astutil.Cursor) bool {
		if !done && c.Node() == old {
			c.Replace(new)
			done = true
		}
		return true
	})
	if !done {
		return
	}
	g.count()
	g.undos = append(g.undos, func() {
		astutil.Apply(f, nil, func(c *astutil.Cursor) bool {
			if c.Node() == new {
				c.Replace(old)
			}
			return true
		})
	})
}

// replaceExpr replaces expression old with new in the current file, with
// undo.
func (g *gen) replaceExpr(old, new ast.Expr) {
	f := g.curFile
	done := false
	astutil.Apply(f, nil, func(c *astutil.Cursor) bool {
		if !done && c.Node() == old {
			c.Replace(new)
			done = true
		}
		return true
	})
	if !done {
		return
	}
	g.count()
	g.undos = append(g.undos, func() {
		astutil.Apply(f, nil, func(c *astutil.Cursor) bool {
			if c.Node() == new {
				c.Replace(old)
			}
			return true
		})
	})
}

// count records one rewritten site for the running feature.
func (g *gen) count() {
	g.sites++
	if g.kinds == nil {
		g.kinds = map[string]int{}
	}
	g.kinds[g.feature]++
}

// skip records why a site was left alone (reported by specaudit).
func (g *gen) skip(reason string) {
	if g.skips == nil {
		g.skips = map[string]int{}
	}
	g.skips[reason]++
}
