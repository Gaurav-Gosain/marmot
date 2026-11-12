package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// listPkg is one package of `go list -deps -test -json`. Test variants are
// separate packages ("p [p.test]", "p_test [p.test]").
type listPkg struct {
	ImportPath      string // unique ID, with the [p.test] suffix for variants
	Name            string
	Dir             string
	ForTest         string
	Standard        bool
	DepOnly         bool
	Goroot          bool
	Module          *listModule
	GoFiles         []string
	CgoFiles        []string
	CompiledGoFiles []string
	IgnoredGoFiles  []string
	OtherFiles      []string // .s and other non-Go files (asmdecl reads them)
	SFiles          []string
	Imports         []string          // resolved IDs of direct imports
	ImportMap       map[string]string // source import path -> resolved ID
	Error           *listError
	DepsErrors      []*listError
	Incomplete      bool

	// Filled after decoding.
	path    string     // package path without the variant suffix
	deps    []*listPkg // direct imports, resolved
	root    bool       // analyzed with every check (not only for facts)
	key     string     // cache key, set by the engine
	cgoDiag bool
}

type listModule struct {
	Path      string
	Version   string
	Main      bool
	Dir       string
	GoMod     string
	GoVersion string
	Replace   *listModule
}

type listError struct {
	Pos string
	Err string
}

// graph is the package graph of one check run.
type graph struct {
	pkgs  []*listPkg          // in go list order (dependencies first)
	byID  map[string]*listPkg // ImportPath -> package
	roots []*listPkg          // the packages every check reports on
}

// goList runs go list for the patterns in dir and builds the graph. Env is
// added to the environment of the go command.
func goList(dir string, patterns []string, tags []string, env []string, stderr io.Writer) (*graph, error) {
	args := []string{"list", "-e", "-deps", "-test", "-compiled", "-json=ImportPath,Name,Dir,ForTest,Standard,DepOnly,Goroot,Module,GoFiles,CgoFiles,CompiledGoFiles,IgnoredGoFiles,OtherFiles,SFiles,Imports,ImportMap,Error,DepsErrors,Incomplete"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, "--")
	args = append(args, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list: %v\n%s", err, strings.TrimSpace(errb.String()))
	}
	var pkgs []*listPkg
	dec := json.NewDecoder(&out)
	for dec.More() {
		p := new(listPkg)
		if err := dec.Decode(p); err != nil {
			return nil, fmt.Errorf("go list output: %v", err)
		}
		pkgs = append(pkgs, p)
	}
	return newGraph(pkgs), nil
}

// newGraph links decoded go list packages into a graph.
func newGraph(pkgs []*listPkg) *graph {
	g := &graph{byID: map[string]*listPkg{}, pkgs: pkgs}
	for _, p := range pkgs {
		p.path = p.ImportPath
		if i := strings.Index(p.path, " ["); i >= 0 {
			p.path = p.path[:i]
		}
		p.deps, p.root = nil, false
		g.byID[p.ImportPath] = p
	}
	for _, p := range g.pkgs {
		for _, id := range p.Imports {
			if d := g.byID[id]; d != nil {
				p.deps = append(p.deps, d)
			}
		}
	}
	g.selectRoots()
	return g
}

// selectRoots picks the packages to report on, as go vet does: for a
// package with tests, its test variant (which has every non-test file too)
// and its external test package; otherwise the package itself. The
// generated test main ("p.test") is never a root.
func (g *graph) selectRoots() {
	hasTestVariant := map[string]bool{}
	for _, p := range g.pkgs {
		if p.DepOnly || p.ForTest == "" {
			continue
		}
		if p.path == p.ForTest {
			hasTestVariant[p.path] = true
		}
	}
	for _, p := range g.pkgs {
		if p.DepOnly || strings.HasSuffix(p.ImportPath, ".test") {
			continue
		}
		if p.ForTest == "" && hasTestVariant[p.path] {
			continue
		}
		if p.ForTest != "" && !g.isMatched(p.ForTest) {
			continue // a test variant of a dependency
		}
		p.root = true
		g.roots = append(g.roots, p)
	}
	sort.Slice(g.roots, func(i, j int) bool { return g.roots[i].ImportPath < g.roots[j].ImportPath })
}

// isMatched reports whether the plain package path was named by the
// patterns.
func (g *graph) isMatched(path string) bool {
	p := g.byID[path]
	return p != nil && !p.DepOnly
}

// files returns the Go files the type checker reads, as absolute paths.
func (p *listPkg) files() []string {
	names := p.CompiledGoFiles
	if len(names) == 0 {
		names = p.GoFiles
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !filepath.IsAbs(n) {
			n = filepath.Join(p.Dir, n)
		}
		out = append(out, n)
	}
	return out
}

// otherFiles returns the non-Go files of the package as absolute paths.
func (p *listPkg) otherFiles() []string {
	var out []string
	for _, n := range append(append([]string(nil), p.SFiles...), p.OtherFiles...) {
		if !filepath.IsAbs(n) {
			n = filepath.Join(p.Dir, n)
		}
		out = append(out, n)
	}
	return out
}

// goVersion is the language version of the package's module, as go vet
// passes it to the type checker ("go1.22"), or "" for the default.
func (p *listPkg) goVersion() string {
	if p.Module == nil || p.Module.GoVersion == "" {
		return ""
	}
	return "go" + p.Module.GoVersion
}

// immutable reports whether the package's files cannot change without its
// version changing: the standard library of a fixed Go version, or a module
// from the module cache. Their cache keys use file names, not contents.
func (p *listPkg) immutable() bool {
	if p.Goroot || p.Standard {
		return true
	}
	m := p.Module
	if m == nil || m.Main {
		return false
	}
	if m.Replace != nil {
		return m.Replace.Version != ""
	}
	return m.Version != ""
}

// listErrors returns the load errors of the package itself.
func (p *listPkg) listErrors() []string {
	var out []string
	if p.Error != nil {
		out = append(out, p.Error.text())
	}
	return out
}

func (e *listError) text() string {
	if e.Pos != "" && !strings.HasPrefix(e.Err, e.Pos) {
		return e.Pos + ": " + e.Err
	}
	return e.Err
}
