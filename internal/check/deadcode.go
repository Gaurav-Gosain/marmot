// The report follows golang.org/x/tools/cmd/deadcode (Copyright 2023 The
// Go Authors, BSD-style license): rapid type analysis from every main and
// test main, reporting the source functions it never reaches, except in
// generated files and marker methods. The analysis runs over per-package
// summaries (deadsum.go).

package check

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
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
		res = deadcode(sc, opts, st)
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

// deadcode lists the workspace packages with their tests and
// dependencies, summarizes each package (from the cache when its inputs
// and its dependencies' export data are unchanged) and runs the
// reachability over the summaries.
func deadcode(sc *scope, opts Options, st *store) deadResult {
	ws := &scope{dir: sc.dir, modules: sc.modules}
	var dirs []string
	for _, m := range sc.modules {
		ws.patterns = append(ws.patterns, m.Dir+"/...")
		dirs = append(dirs, m.Dir)
	}
	g, _, err := cachedGoList(st, ws, opts.Tags, nil, true)
	if err != nil {
		return deadResult{Err: "deadcode: " + firstLine(err.Error())}
	}
	var errs []string
	for _, p := range g.pkgs {
		for _, le := range p.listErrors() {
			errs = append(errs, le)
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return deadResult{Err: fmt.Sprintf("deadcode: the packages have errors (%d), first: %s", len(errs), errs[0])}
	}
	// Only modules with a program: in a library module every function no
	// test calls is unreachable, which is its API, not dead code.
	modules := map[string]bool{}
	all := map[string]bool{}
	var roots []string
	for _, p := range g.pkgs {
		if p.DepOnly || p.Module == nil || p.Module.Path == "" {
			continue
		}
		all[p.Module.Path] = true
		if p.Name != "main" {
			continue
		}
		roots = append(roots, p.path+".init", p.path+".main")
		if p.ForTest == "" && !strings.HasSuffix(p.ImportPath, ".test") {
			modules[p.Module.Path] = true
		}
	}
	var libs []string
	for m := range all {
		if !modules[m] {
			libs = append(libs, m)
		}
	}
	sort.Strings(libs)
	if len(roots) == 0 {
		return deadResult{Libs: libs}
	}
	inModules := func(path string) bool {
		for m := range modules {
			if path == m || strings.HasPrefix(path, m+"/") {
				return true
			}
		}
		return false
	}
	e := newEngine(g, st, newSuite(nil, toolID(), ""), envKey(opts.Tags, hostGOOS(), hostGOARCH()), hostGOARCH(), opts.Jobs)
	e.sums, e.wsDirs = true, dirs
	sums := make([]*deadSummary, len(g.pkgs))
	sumErrs := make([]error, len(g.pkgs))
	var wg sync.WaitGroup
	for i, p := range g.pkgs {
		if p.ImportPath == "unsafe" {
			continue
		}
		wg.Add(1)
		go func(i int, p *listPkg) {
			defer wg.Done()
			sums[i], sumErrs[i] = e.summary(p)
		}(i, p)
	}
	wg.Wait()
	for i, err := range sumErrs {
		if err != nil {
			errs = append(errs, g.pkgs[i].ImportPath+": "+firstLine(err.Error()))
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return deadResult{Err: fmt.Sprintf("deadcode: the packages have errors (%d), first: %s", len(errs), errs[0])}
	}
	reached := reach(sums, roots)
	var out []deadFunc
	seen := map[string]bool{}
	for i, s := range sums {
		p := g.pkgs[i]
		if s == nil || p.DepOnly || !inModules(p.path) {
			continue
		}
		for _, d := range s.Decls {
			k := d.Key + " " + fmt.Sprint(d.Span)
			if seen[k] || reached[d.Key] || d.Generated || d.Marker {
				continue
			}
			seen[k] = true
			out = append(out, deadFunc{Span: d.Span, Name: d.Name, Pkg: p.path})
		}
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
