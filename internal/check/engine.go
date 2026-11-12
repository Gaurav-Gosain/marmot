package check

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/gcexportdata"
)

// engineVersion changes whenever the cached form or the meaning of a
// cached result changes.
const engineVersion = "check-engine-1"

// finding is one analyzer diagnostic in a position independent form, as it
// is cached.
type finding struct {
	Analyzer string
	Category string
	Span     diag.Span
	Message  string
	URL      string
	Fixes    []rawFix
	Related  []diag.Related
}

type rawFix struct {
	Message string
	Edits   []diag.Edit
}

// nolintRange is a //nolint directive: the lines it covers and the linters
// it names (none means all).
type nolintRange struct {
	File     string
	From, To int
	Linters  []string
}

// factsEntry is what importers of a package need: its types in export
// data form and the facts its analysis exported.
type factsEntry struct {
	Types []byte
	Facts []byte
}

// report is the cached result of analyzing a root package.
type report struct {
	Findings   []finding
	TypeErrors []finding // as findings of the pseudo analyzer "typecheck"
	Errors     []string  // analyzers that failed
	Generated  []string  // files with a generated code header
	Nolint     []nolintRange
}

// suite is the set of analyzers a run uses.
type suite struct {
	all   []*analysis.Analyzer // run on root packages
	facts []*analysis.Analyzer // run on dependencies (those that use facts)
	// factsConfig identifies the fact producing analyzers (it is part of
	// every facts key, so a change of the reporting analyzers or their
	// settings does not recompute the facts of the standard library);
	// reportConfig identifies everything else.
	factsConfig, reportConfig string
}

func newSuite(as []*analysis.Analyzer, tool, config string) *suite {
	s := &suite{}
	seen := map[*analysis.Analyzer]bool{}
	for _, a := range as {
		if !seen[a] {
			seen[a] = true
			s.all = append(s.all, a)
		}
	}
	uses := map[*analysis.Analyzer]bool{}
	var usesFacts func(a *analysis.Analyzer) bool
	usesFacts = func(a *analysis.Analyzer) bool {
		if v, ok := uses[a]; ok {
			return v
		}
		v := len(a.FactTypes) > 0
		for _, f := range a.FactTypes {
			gob.Register(f)
		}
		for _, r := range a.Requires {
			if usesFacts(r) {
				v = true
			}
		}
		uses[a] = v
		return v
	}
	var fnames, anames []string
	for _, a := range s.all {
		if usesFacts(a) {
			s.facts = append(s.facts, a)
		}
	}
	// The prerequisites run on dependencies too; name them all.
	var walk func(a *analysis.Analyzer, out *[]string, seen map[*analysis.Analyzer]bool)
	walk = func(a *analysis.Analyzer, out *[]string, seen map[*analysis.Analyzer]bool) {
		if seen[a] {
			return
		}
		seen[a] = true
		*out = append(*out, a.Name)
		for _, r := range a.Requires {
			walk(r, out, seen)
		}
	}
	fs, as2 := map[*analysis.Analyzer]bool{}, map[*analysis.Analyzer]bool{}
	for _, a := range s.facts {
		walk(a, &fnames, fs)
	}
	for _, a := range s.all {
		walk(a, &anames, as2)
	}
	sort.Strings(fnames)
	sort.Strings(anames)
	h := newHasher("facts-config")
	h.str(tool)
	h.str(fnames...)
	s.factsConfig = h.sum()
	r := newHasher("report-config")
	r.str(tool, config)
	r.str(anames...)
	s.reportConfig = r.sum()
	return s
}

// engine analyzes the packages of a graph, reusing cached results.
type engine struct {
	g      *graph
	st     *store
	suite  *suite
	env    string // GOOS, GOARCH, tags, Go version
	goarch string
	sem    chan struct{}

	mu    sync.Mutex
	units map[*listPkg]*unit

	// Stats.
	hits, misses, analyzed int
	slow                   []slowPkg
}

type slowPkg struct {
	ID  string
	Dur time.Duration
}

type unit struct {
	factsOnce sync.Once
	facts     *factsEntry
	factsErr  error

	fullOnce   sync.Once
	fullFacts  *factsEntry
	fullReport *report
	fullErr    error

	factsKey  string
	reportKey string
}

func newEngine(g *graph, st *store, s *suite, env, goarch string, jobs int) *engine {
	if jobs <= 0 {
		jobs = runtime.GOMAXPROCS(0)
	}
	e := &engine{g: g, st: st, suite: s, env: env, goarch: goarch, sem: make(chan struct{}, jobs), units: map[*listPkg]*unit{}}
	for _, p := range g.pkgs {
		e.units[p] = &unit{}
	}
	e.computeKeys()
	return e
}

// computeKeys sets the facts and report keys of every package. go list
// prints dependencies before their importers, so one pass in order works.
func (e *engine) computeKeys() {
	// Hash the files of mutable packages in parallel first.
	hashes := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, p := range e.g.pkgs {
		if p.immutable() {
			continue
		}
		for _, f := range append(p.files(), p.otherFiles()...) {
			wg.Add(1)
			sem <- struct{}{}
			go func(f string) {
				defer wg.Done()
				h := fileHash(f)
				mu.Lock()
				hashes[f] = h
				mu.Unlock()
				<-sem
			}(f)
		}
	}
	wg.Wait()
	for _, p := range e.g.pkgs {
		u := e.units[p]
		h := newHasher("facts")
		h.str(engineVersion, runtime.Version(), e.env, e.suite.factsConfig, p.ImportPath, p.Name, p.goVersion())
		if p.Module != nil {
			h.str("module", p.Module.Path, p.Module.Version)
		}
		for _, f := range append(p.files(), p.otherFiles()...) {
			h.str(f)
			if !p.immutable() {
				h.str(hashes[f])
			}
		}
		for _, err := range p.listErrors() {
			h.str("error", err)
		}
		ids := make([]string, 0, len(p.ImportMap))
		for src, id := range p.ImportMap {
			ids = append(ids, src+"="+id)
		}
		sort.Strings(ids)
		h.str(ids...)
		deps := slices.Clone(p.deps)
		sort.Slice(deps, func(i, j int) bool { return deps[i].ImportPath < deps[j].ImportPath })
		for _, d := range deps {
			h.str(d.ImportPath, e.units[d].factsKey)
		}
		u.factsKey = h.sum()
		r := newHasher("report")
		r.str(u.factsKey, e.suite.reportConfig)
		u.reportKey = r.sum()
	}
}

// report returns the analysis of a root package, from the cache or by
// running every analyzer.
func (e *engine) report(p *listPkg) (*report, error) {
	u := e.units[p]
	if data := e.st.get(u.reportKey); data != nil {
		var r report
		if gob.NewDecoder(bytes.NewReader(data)).Decode(&r) == nil {
			e.count(&e.hits)
			return &r, nil
		}
	}
	e.count(&e.misses)
	_, r, err := e.full(p)
	return r, err
}

// full analyzes a root package with every analyzer, once, and keeps both
// results: the report, and the facts entry its importers (an external
// test package imports the test variant) need.
func (e *engine) full(p *listPkg) (*factsEntry, *report, error) {
	u := e.units[p]
	u.fullOnce.Do(func() {
		u.fullFacts, u.fullReport, u.fullErr = e.analyze(p, true)
		if u.fullErr == nil {
			e.putGob(u.factsKey, u.fullFacts)
			e.putGob(u.reportKey, u.fullReport)
		}
	})
	return u.fullFacts, u.fullReport, u.fullErr
}

// factsOf returns the facts entry of a dependency.
func (e *engine) factsOf(p *listPkg) (*factsEntry, error) {
	u := e.units[p]
	u.factsOnce.Do(func() {
		if data := e.st.get(u.factsKey); data != nil {
			var fe factsEntry
			if gob.NewDecoder(bytes.NewReader(data)).Decode(&fe) == nil {
				u.facts = &fe
				return
			}
		}
		if p.root {
			u.facts, _, u.factsErr = e.full(p)
			return
		}
		fe, _, err := e.analyze(p, false)
		if err != nil {
			u.factsErr = err
			return
		}
		u.facts = fe
		e.putGob(u.factsKey, fe)
	})
	return u.facts, u.factsErr
}

func (e *engine) putGob(key string, v any) {
	var buf bytes.Buffer
	if gob.NewEncoder(&buf).Encode(v) == nil {
		e.st.put(key, buf.Bytes())
	}
}

func (e *engine) count(n *int) {
	e.mu.Lock()
	*n++
	e.mu.Unlock()
}

// analyze type checks one package and runs the analyzers on it: all of
// them for a root, only the fact producing ones for a dependency.
func (e *engine) analyze(p *listPkg, root bool) (*factsEntry, *report, error) {
	// Dependencies first, in parallel, outside the job slot.
	deps := map[string]*factsEntry{} // by resolved ID
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	for _, d := range p.deps {
		if d.ImportPath == "unsafe" {
			continue
		}
		wg.Add(1)
		go func(d *listPkg) {
			defer wg.Done()
			fe, err := e.factsOf(d)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("%s: %v", d.ImportPath, err)
			}
			deps[d.ImportPath] = fe
		}(d)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, nil, firstErr
	}
	e.sem <- struct{}{}
	defer func() { <-e.sem }()
	e.count(&e.analyzed)
	t0 := time.Now()
	fe, r, err := e.run(p, root, deps)
	if d := time.Since(t0); d > 500*time.Millisecond {
		e.mu.Lock()
		e.slow = append(e.slow, slowPkg{p.ImportPath, d})
		e.mu.Unlock()
	}
	return fe, r, err
}

func (e *engine) run(p *listPkg, root bool, deps map[string]*factsEntry) (*factsEntry, *report, error) {
	fset := token.NewFileSet()
	rep := &report{}
	var files []*ast.File
	for _, name := range p.files() {
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			if f == nil {
				return nil, nil, err
			}
			rep.TypeErrors = append(rep.TypeErrors, parseErrors(err)...)
		}
		files = append(files, f)
	}
	// The dependencies by package path, for facts.
	byPath := map[string]*factsEntry{}
	imports := map[string]*types.Package{}
	importer := importerFunc(func(path string) (*types.Package, error) {
		id := path
		if m, ok := p.ImportMap[path]; ok {
			id = m
		}
		if id == "unsafe" {
			return types.Unsafe, nil
		}
		d := e.g.byID[id]
		fe := deps[id]
		if d == nil || fe == nil {
			return nil, fmt.Errorf("cannot find package %q", path)
		}
		byPath[d.path] = fe
		if pkg, ok := imports[d.path]; ok && pkg.Complete() {
			return pkg, nil
		}
		return gcexportdata.Read(bytes.NewReader(fe.Types), fset, imports, d.path)
	})
	var typeErrs []types.Error
	tc := &types.Config{
		Importer:  importer,
		Sizes:     types.SizesFor("gc", e.goarch),
		GoVersion: p.goVersion(),
		Error: func(err error) {
			if te, ok := err.(types.Error); ok {
				typeErrs = append(typeErrs, te)
			}
		},
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
	pkg, _ := tc.Check(p.path, fset, files, info)
	for _, te := range typeErrs {
		pos := fset.Position(te.Pos)
		if strings.HasPrefix(te.Msg, "\t") && len(rep.TypeErrors) > 0 {
			// A continuation of the previous error ("\tother declaration
			// of x").
			last := &rep.TypeErrors[len(rep.TypeErrors)-1]
			last.Related = append(last.Related, diag.Related{Span: diag.Span{File: pos.Filename, Line: pos.Line, Col: pos.Column, EndLine: pos.Line, EndCol: pos.Column}, Message: strings.TrimSpace(te.Msg)})
			continue
		}
		rep.TypeErrors = append(rep.TypeErrors, finding{
			Analyzer: "typecheck",
			Span:     diag.Span{File: pos.Filename, Line: pos.Line, Col: pos.Column, EndLine: pos.Line, EndCol: pos.Column},
			Message:  te.Msg,
		})
	}
	hasErrors := len(rep.TypeErrors) > 0 || len(p.listErrors()) > 0

	facts, err := decodeFacts(pkg, func(path string) *types.Package { return imports[path] }, func(path string) []byte {
		if fe := byPath[path]; fe != nil {
			return fe.Facts
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	analyzers := e.suite.facts
	if root {
		analyzers = e.suite.all
	}
	results := e.exec(p, fset, files, pkg, info, tc.Sizes, typeErrs, facts, analyzers, hasErrors, rep)

	fe := &factsEntry{}
	var tbuf bytes.Buffer
	if err := gcexportdata.Write(&tbuf, fset, pkg); err != nil {
		return nil, nil, fmt.Errorf("export %s: %v", p.ImportPath, err)
	}
	fe.Types = tbuf.Bytes()
	if fe.Facts, err = facts.encode(); err != nil {
		return nil, nil, err
	}
	if !root {
		return fe, nil, nil
	}
	rep.Findings = results
	for _, f := range files {
		name := fset.File(f.FileStart).Name()
		if ast.IsGenerated(f) {
			rep.Generated = append(rep.Generated, name)
		}
		rep.Nolint = append(rep.Nolint, nolintRanges(fset, f)...)
	}
	sortFindings(rep.Findings)
	sortFindings(rep.TypeErrors)
	return fe, rep, nil
}

// exec runs the analyzers and their prerequisites on one package and
// returns the findings of the requested analyzers.
func (e *engine) exec(p *listPkg, fset *token.FileSet, files []*ast.File, pkg *types.Package, info *types.Info, sizes types.Sizes, typeErrs []types.Error, facts *factSet, analyzers []*analysis.Analyzer, hasErrors bool, rep *report) []finding {
	type action struct {
		once     sync.Once
		result   any
		err      error
		findings []finding
	}
	actions := map[*analysis.Analyzer]*action{}
	var add func(a *analysis.Analyzer)
	add = func(a *analysis.Analyzer) {
		if actions[a] != nil {
			return
		}
		actions[a] = &action{}
		for _, r := range a.Requires {
			add(r)
		}
	}
	for _, a := range analyzers {
		add(a)
	}
	var module *analysis.Module
	if p.Module != nil {
		module = &analysis.Module{Path: p.Module.Path, Version: p.Module.Version, GoVersion: p.Module.GoVersion}
	}
	other := p.otherFiles()
	ignored := make([]string, 0, len(p.IgnoredGoFiles))
	for _, n := range p.IgnoredGoFiles {
		ignored = append(ignored, joinDir(p.Dir, n))
	}
	readable := map[string]bool{}
	for _, n := range append(append(slices.Clone(other), ignored...), p.files()...) {
		readable[n] = true
	}
	var exec func(a *analysis.Analyzer) *action
	exec = func(a *analysis.Analyzer) *action {
		act := actions[a]
		act.once.Do(func() {
			var wg sync.WaitGroup
			for _, r := range a.Requires {
				wg.Add(1)
				go func(r *analysis.Analyzer) { defer wg.Done(); exec(r) }(r)
			}
			wg.Wait()
			inputs := map[*analysis.Analyzer]any{}
			var failed []string
			for _, r := range a.Requires {
				ra := actions[r]
				if ra.err == errSkipped {
					act.err = errSkipped
					return
				}
				if ra.err != nil {
					failed = append(failed, r.Name)
					continue
				}
				inputs[r] = ra.result
			}
			if failed != nil {
				sort.Strings(failed)
				act.err = fmt.Errorf("failed prerequisites: %s", strings.Join(failed, ", "))
				return
			}
			if hasErrors && !a.RunDespiteErrors {
				act.err = errSkipped
				return
			}
			filter := map[reflect.Type]bool{}
			for _, f := range a.FactTypes {
				filter[reflect.TypeOf(f)] = true
			}
			pass := &analysis.Pass{
				Analyzer:          a,
				Fset:              fset,
				Files:             files,
				OtherFiles:        other,
				IgnoredFiles:      ignored,
				Pkg:               pkg,
				TypesInfo:         info,
				TypesSizes:        sizes,
				TypeErrors:        typeErrs,
				ResultOf:          inputs,
				ImportObjectFact:  facts.importObjectFact,
				ExportObjectFact:  facts.exportObjectFact,
				AllObjectFacts:    func() []analysis.ObjectFact { return facts.allObjectFacts(filter) },
				ImportPackageFact: facts.importPackageFact,
				ExportPackageFact: facts.exportPackageFact,
				AllPackageFacts:   func() []analysis.PackageFact { return facts.allPackageFacts(filter) },
				Module:            module,
			}
			pass.ReadFile = func(name string) ([]byte, error) {
				if !readable[name] {
					return nil, fmt.Errorf("Pass.ReadFile: %s is not a file of the package", name)
				}
				return os.ReadFile(name)
			}
			var mu sync.Mutex
			pass.Report = func(d analysis.Diagnostic) {
				f := toFinding(fset, a, d)
				mu.Lock()
				act.findings = append(act.findings, f)
				mu.Unlock()
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						act.err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
					}
				}()
				t0 := time.Now()
				act.result, act.err = a.Run(pass)
				if d := time.Since(t0); d > 300*time.Millisecond {
					e.mu.Lock()
					e.slow = append(e.slow, slowPkg{p.ImportPath + " " + a.Name, d})
					e.mu.Unlock()
				}
			}()
		})
		return act
	}
	var wg sync.WaitGroup
	for _, a := range analyzers {
		wg.Add(1)
		go func(a *analysis.Analyzer) { defer wg.Done(); exec(a) }(a)
	}
	wg.Wait()
	var out []finding
	for _, a := range analyzers {
		act := actions[a]
		if act.err != nil && act.err != errSkipped {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %s: %v", p.ImportPath, a.Name, firstLine(act.err.Error())))
		}
		out = append(out, act.findings...)
	}
	sort.Strings(rep.Errors)
	return out
}

var errSkipped = fmt.Errorf("skipped: the package has errors")

func toFinding(fset *token.FileSet, a *analysis.Analyzer, d analysis.Diagnostic) finding {
	f := finding{Analyzer: a.Name, Category: d.Category, Message: d.Message, URL: d.URL, Span: span(fset, d.Pos, d.End)}
	for _, sf := range d.SuggestedFixes {
		rf := rawFix{Message: sf.Message}
		for _, te := range sf.TextEdits {
			s := span(fset, te.Pos, te.End)
			rf.Edits = append(rf.Edits, diag.Edit{File: s.File, Span: s, NewText: string(te.NewText)})
		}
		f.Fixes = append(f.Fixes, rf)
	}
	for _, r := range d.Related {
		f.Related = append(f.Related, diag.Related{Span: span(fset, r.Pos, r.End), Message: r.Message})
	}
	return f
}

func span(fset *token.FileSet, pos, end token.Pos) diag.Span {
	if !pos.IsValid() {
		return diag.Span{}
	}
	a := fset.Position(pos)
	s := diag.Span{File: a.Filename, Line: a.Line, Col: a.Column, EndLine: a.Line, EndCol: a.Column}
	if end.IsValid() && end >= pos {
		b := fset.Position(end)
		if b.Filename == a.Filename {
			s.EndLine, s.EndCol = b.Line, b.Column
		}
	}
	return s
}

func parseErrors(err error) []finding {
	var out []finding
	add := func(pos token.Position, msg string) {
		out = append(out, finding{Analyzer: "typecheck", Span: diag.Span{File: pos.Filename, Line: pos.Line, Col: pos.Column, EndLine: pos.Line, EndCol: pos.Column}, Message: msg})
	}
	if list, ok := err.(scanner.ErrorList); ok {
		for _, e := range list {
			add(e.Pos, e.Msg)
		}
		return out
	}
	add(token.Position{}, err.Error())
	return out
}

func sortFindings(fs []finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Span.File != b.Span.File {
			return a.Span.File < b.Span.File
		}
		if a.Span.Line != b.Span.Line {
			return a.Span.Line < b.Span.Line
		}
		if a.Span.Col != b.Span.Col {
			return a.Span.Col < b.Span.Col
		}
		if a.Analyzer != b.Analyzer {
			return a.Analyzer < b.Analyzer
		}
		return a.Message < b.Message
	})
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

func joinDir(dir, name string) string {
	if strings.HasPrefix(name, "/") {
		return name
	}
	return dir + string(os.PathSeparator) + name
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
