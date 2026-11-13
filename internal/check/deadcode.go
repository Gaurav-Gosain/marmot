// The reachability analysis follows golang.org/x/tools/cmd/deadcode
// (Copyright 2023 The Go Authors, BSD-style license): rapid type analysis
// from every main and test main, reporting the source functions it never
// reaches, except in generated files and marker methods.

package check

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

type deadFunc struct {
	Span diag.Span
	Name string
	Pkg  string
}

type deadResult struct {
	Funcs []deadFunc
	Libs  []string // modules without a main package, not reported
	Err   string
}

// runDeadcode reports functions of the workspace modules that no main
// package or test can reach. The result is cached by the module trees
// (file names, sizes and times) and the build configuration. In Changed
// mode only functions in changed files are reported.
func runDeadcode(sc *scope, opts Options, st *store, r *CheckResult) {
	if len(sc.modules) == 0 {
		return
	}
	h := newHasher("deadcode-2")
	h.str(toolID(), sc.dir, envKey(opts.Tags, hostGOOS(), hostGOARCH()))
	files := sc.treeFiles()
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		h.str(p, files[p])
	}
	key := h.sum()
	// The shape key: the reachability inputs of every package, from the
	// analysis. When it matches, only positions may have moved.
	skey := ""
	if sc.shapes != nil {
		sh := newHasher("deadcode-shape")
		sh.str(toolID(), sc.dir, envKey(opts.Tags, hostGOOS(), hostGOARCH()))
		ids := make([]string, 0, len(sc.shapes))
		for id, s := range sc.shapes {
			ids = append(ids, id+" "+s)
		}
		sort.Strings(ids)
		sh.str(ids...)
		for _, m := range sc.modules {
			for _, n := range []string{"go.mod", "go.sum"} {
				data, _ := os.ReadFile(filepath.Join(m.Dir, n))
				sh.bytes(data)
			}
		}
		skey = sh.sum()
	}
	var res deadResult
	switch {
	case getGob(st, key, &res):
		r.Cached = true
	case skey != "" && getGob(st, skey, &res):
		r.Cached = true
		res.Funcs = relocate(res.Funcs)
		putGob(st, key, res)
	default:
		res = deadcode(sc, opts)
		if res.Err == "" {
			putGob(st, key, res)
			if skey != "" {
				putGob(st, skey, res)
			}
		}
	}
	if res.Err != "" {
		r.Status = Failed
		r.Notes = append(r.Notes, res.Err)
		return
	}
	if len(res.Libs) > 0 {
		r.Notes = append(r.Notes, "library modules (no main package) not reported: "+strings.Join(res.Libs, ", "))
	}
	for _, f := range res.Funcs {
		if sc.changed != nil && !sc.changed[f.Span.File] {
			continue
		}
		r.Findings = append(r.Findings, diag.Diagnostic{
			Kind: "diagnostic", Severity: diag.Warning, Code: diag.CodeDeadcode, Check: "deadcode", Analyzer: "deadcode",
			Message: "unreachable func: " + f.Name, Span: f.Span, Package: f.Pkg,
		})
	}
}

func deadcode(sc *scope, opts Options) deadResult {
	var patterns []string
	for _, m := range sc.modules {
		patterns = append(patterns, m.Dir+"/...")
	}
	cfg := &packages.Config{
		Mode:  packages.LoadAllSyntax | packages.NeedModule,
		Tests: true,
		Dir:   sc.dir,
		Env:   append(os.Environ(), "PWD="+sc.dir),
	}
	if len(opts.Tags) > 0 {
		cfg.BuildFlags = []string{"-tags=" + strings.Join(opts.Tags, ",")}
	}
	initial, err := packages.Load(cfg, patterns...)
	if err != nil {
		return deadResult{Err: "deadcode: " + firstLine(err.Error())}
	}
	var errs []string
	packages.Visit(initial, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			errs = append(errs, e.Error())
		}
	})
	if len(errs) > 0 {
		sort.Strings(errs)
		return deadResult{Err: fmt.Sprintf("deadcode: the packages have errors (%d), first: %s", len(errs), errs[0])}
	}
	// Only modules with a program: in a library module every function no
	// test calls is unreachable, which is its API, not dead code.
	modules := map[string]bool{}
	var libs []string
	all := map[string]bool{}
	for _, p := range initial {
		if p.Module == nil || p.Module.Path == "" {
			continue
		}
		all[p.Module.Path] = true
		if p.Name == "main" && !strings.HasSuffix(p.ID, ".test") && !strings.Contains(p.ID, " [") {
			modules[p.Module.Path] = true
		}
	}
	for m := range all {
		if !modules[m] {
			libs = append(libs, m)
		}
	}
	sort.Strings(libs)
	inModules := func(path string) bool {
		for m := range modules {
			if path == m || strings.HasPrefix(path, m+"/") {
				return true
			}
		}
		return false
	}
	prog, pkgs := ssautil.AllPackages(initial, ssa.InstantiateGenerics)
	mains := ssautil.MainPackages(pkgs)
	if len(mains) == 0 {
		return deadResult{}
	}
	var roots []*ssa.Function
	for _, m := range mains {
		roots = append(roots, m.Func("init"), m.Func("main"))
	}
	var sourceFuncs []*ssa.Function
	generated := map[string]bool{}
	ifaces := map[*types.Package][]*types.Interface{}
	packages.Visit(initial, nil, func(p *packages.Package) {
		var is []*types.Interface
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			if tn, ok := scope.Lookup(name).(*types.TypeName); ok && types.IsInterface(tn.Type()) {
				is = append(is, tn.Type().Underlying().(*types.Interface))
			}
		}
		ifaces[p.Types] = is
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok {
					if obj, ok := p.TypesInfo.Defs[fd.Name].(*types.Func); ok {
						sourceFuncs = append(sourceFuncs, prog.FuncValue(obj))
					}
				}
			}
			if ast.IsGenerated(f) {
				generated[p.Fset.File(f.Pos()).Name()] = true
			}
		}
	})
	prog.Build()
	fset := prog.Fset
	res := rta.Analyze(roots, false)
	reachable := map[token.Position]bool{}
	for fn := range res.Reachable {
		if fn.Pos().IsValid() || fn.Name() == "init" {
			reachable[fset.Position(fn.Pos())] = true
		}
	}
	var out []deadFunc
	for _, fn := range sourceFuncs {
		if fn == nil || fn.Pkg == nil {
			continue
		}
		pos := fset.Position(fn.Pos())
		if reachable[pos] {
			continue
		}
		reachable[pos] = true
		if !inModules(fn.Pkg.Pkg.Path()) || generated[pos.Filename] || isMarkerMethod(fn, ifaces[fn.Pkg.Pkg]) {
			continue
		}
		out = append(out, deadFunc{
			Span: diag.Span{File: pos.Filename, Line: pos.Line, Col: pos.Column, EndLine: pos.Line, EndCol: pos.Column},
			Name: prettyName(fn),
			Pkg:  fn.Pkg.Pkg.Path(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Span, out[j].Span
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line || a.Line == b.Line && a.Col < b.Col
	})
	return deadResult{Funcs: out, Libs: libs}
}

func prettyName(fn *ssa.Function) string {
	var b strings.Builder
	var format func(*ssa.Function)
	format = func(fn *ssa.Function) {
		if fn.Parent() != nil {
			format(fn.Parent())
			fmt.Fprintf(&b, "$%d", slices.Index(fn.Parent().AnonFuncs, fn)+1)
			return
		}
		if obj, ok := fn.Object().(*types.Func); ok {
			if recv := obj.Signature().Recv(); recv != nil {
				t := recv.Type()
				if p, ok := t.(*types.Pointer); ok {
					t = p.Elem()
				}
				if n, ok := types.Unalias(t).(*types.Named); ok {
					b.WriteString(n.Obj().Name())
					b.WriteByte('.')
				}
			}
		}
		b.WriteString(fn.Name())
	}
	format(fn)
	return b.String()
}

func isMarkerMethod(fn *ssa.Function, ifaces []*types.Interface) bool {
	if !(fn.Signature.Recv() != nil && !ast.IsExported(fn.Name()) && fn.Signature.Params() == nil && fn.Signature.Results() == nil) {
		return false
	}
	fd, ok := fn.Syntax().(*ast.FuncDecl)
	if !ok || fd.Body == nil || len(fd.Body.List) > 0 {
		return false
	}
	return slices.ContainsFunc(ifaces, func(i *types.Interface) bool {
		return types.Implements(fn.Signature.Recv().Type(), i)
	})
}

func getGob(st *store, key string, v any) bool {
	c := st.get(key)
	return c != nil && gob.NewDecoder(bytes.NewReader(c)).Decode(v) == nil
}

func putGob(st *store, key string, v any) {
	var buf bytes.Buffer
	if gob.NewEncoder(&buf).Encode(v) == nil {
		st.put(key, buf.Bytes())
	}
}

// relocate finds the declarations of dead functions again after their
// files changed in ways that keep the program's shape.
func relocate(fs []deadFunc) []deadFunc {
	files := map[string]map[string]token.Position{}
	out := fs[:0:0]
	for _, f := range fs {
		decls, ok := files[f.Span.File]
		if !ok {
			decls = map[string]token.Position{}
			fset := token.NewFileSet()
			if af, err := parser.ParseFile(fset, f.Span.File, nil, parser.SkipObjectResolution); err == nil {
				for _, d := range af.Decls {
					fd, ok := d.(*ast.FuncDecl)
					if !ok {
						continue
					}
					name := fd.Name.Name
					if fd.Recv != nil && len(fd.Recv.List) > 0 {
						t := fd.Recv.List[0].Type
						if s, ok := t.(*ast.StarExpr); ok {
							t = s.X
						}
						switch x := t.(type) {
						case *ast.IndexExpr:
							t = x.X
						case *ast.IndexListExpr:
							t = x.X
						}
						if id, ok := t.(*ast.Ident); ok {
							name = id.Name + "." + name
						}
					}
					decls[name] = fset.Position(fd.Name.Pos())
				}
			}
			files[f.Span.File] = decls
		}
		pos, ok := decls[f.Name]
		if !ok {
			continue
		}
		f.Span = diag.Span{File: f.Span.File, Line: pos.Line, Col: pos.Column, EndLine: pos.Line, EndCol: pos.Column}
		out = append(out, f)
	}
	return out
}
