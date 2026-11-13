package check

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"golang.org/x/tools/imports"
)

// fmtResult is the cached outcome for one file version.
type fmtResult struct {
	Formatted []byte // nil when the file is already formatted
	Err       string
}

// runFmt checks that every Go file is formatted as gofmt and goimports
// would leave it. In Changed mode only the changed files are read.
func runFmt(sc *scope, st *store, r *CheckResult) {
	files := sc.changedGo
	if sc.changed == nil {
		files = goFiles(sc)
	}
	src := diag.NewSources()
	type out struct {
		d  *diag.Diagnostic
		ok bool
	}
	res := make([]out, len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, f string) {
			defer wg.Done()
			defer func() { <-sem }()
			info, err := os.Stat(f)
			if err != nil {
				return
			}
			// Keyed by size and time: an unchanged file is not read.
			h := newHasher("fmt")
			h.str(toolID(), f, fmt.Sprint(info.Size(), info.ModTime().UnixNano()))
			key := h.sum()
			var fr fmtResult
			var data []byte
			if c := st.get(key); c == nil || gob.NewDecoder(bytes.NewReader(c)).Decode(&fr) != nil {
				if data, err = os.ReadFile(f); err != nil {
					return
				}
				fr = fmtResult{}
				got, err := formatFile(src, f, data)
				if err != nil {
					fr.Err = err.Error()
				} else if !bytes.Equal(got, data) {
					fr.Formatted = got
				}
				var buf bytes.Buffer
				if gob.NewEncoder(&buf).Encode(fr) == nil {
					st.put(key, buf.Bytes())
				}
			}
			if fr.Err != "" {
				d := diagFromText(fr.Err, filepath.Dir(f))
				if !d.Span.Valid() {
					d.Span = diag.Span{File: f, Line: 1, Col: 1, EndLine: 1, EndCol: 1}
				}
				d.Check, d.Code, d.Analyzer = "fmt", diag.CodeNotFmt, "gofmt"
				res[i] = out{d: &d}
				return
			}
			if fr.Formatted == nil {
				return
			}
			if data == nil {
				if data, err = os.ReadFile(f); err != nil {
					return
				}
			}
			d := fmtDiag(f, data, fr.Formatted)
			res[i] = out{d: &d}
		}(i, f)
	}
	wg.Wait()
	for _, o := range res {
		if o.d != nil {
			r.Findings = append(r.Findings, *o.d)
		}
	}
}

// fmtDiag reports a file that is not formatted, with the first changed
// line as its position and the whole new text as one fix.
func fmtDiag(file string, old, new []byte) diag.Diagnostic {
	ol, nl := strings.Split(string(old), "\n"), strings.Split(string(new), "\n")
	line := 1
	for line <= len(ol) && line <= len(nl) && ol[line-1] == nl[line-1] {
		line++
	}
	what := "gofmt"
	if gofmtOnly(old) != nil && bytes.Equal(gofmtOnly(old), old) {
		what = "goimports"
	}
	lastLine := len(ol)
	lastCol := len(ol[lastLine-1]) + 1
	return diag.Diagnostic{
		Kind:     "diagnostic",
		Severity: diag.Warning,
		Code:     diag.CodeNotFmt,
		Check:    "fmt",
		Analyzer: what,
		Message:  "file is not formatted (" + what + ")",
		Span:     diag.Span{File: file, Line: line, Col: 1, EndLine: line, EndCol: 1},
		Fixes: []diag.Fix{{
			Title:         "format the file",
			Applicability: diag.MachineApplicable,
			Edits:         []diag.Edit{{File: file, Span: diag.Span{File: file, Line: 1, Col: 1, EndLine: lastLine, EndCol: lastCol}, NewText: string(new)}},
		}},
	}
}

// goFiles lists the Go files of the scope's modules: the files git tracks
// and the new ones it does not ignore, or every .go file outside hidden
// directories when the tree is not a git checkout.
func goFiles(sc *scope) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] && diag.IsGoFile(p) && sc.fmtFile(p) {
			seen[p] = true
			out = append(out, p)
		}
	}
	top := gitOut(sc.dir, "rev-parse", "--show-toplevel")
	if top != "" {
		ls := gitOut(sc.dir, "ls-files", "--full-name", "--cached", "--others", "--exclude-standard", "--", "*.go")
		for _, n := range strings.Split(ls, "\n") {
			if n == "" {
				continue
			}
			p := filepath.Join(top, n)
			if !strings.HasPrefix(p, sc.dir+string(os.PathSeparator)) {
				continue
			}
			if _, err := os.Stat(p); err == nil {
				add(p)
			}
		}
	} else {
		filepath.WalkDir(sc.dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && p != sc.dir && (strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_")) {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				add(p)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// fmtFile reports whether `go fmt` with the scope's patterns formats the
// file: it is in a directory a pattern names, and not under testdata or a
// directory whose name starts with "." or "_", which the go command skips.
func (sc *scope) fmtFile(p string) bool {
	rel, err := filepath.Rel(sc.dir, filepath.Dir(p))
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel != "." {
		for _, part := range strings.Split(rel, "/") {
			if part == "testdata" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") {
				return false
			}
		}
	}
	for _, pat := range sc.patterns {
		if pat != "." && !strings.HasPrefix(pat, "./") {
			return true // an import path: not narrowed by directory
		}
		pat = strings.TrimPrefix(strings.TrimPrefix(pat, "."), "/")
		if base, ok := strings.CutSuffix(pat, "..."); ok {
			base = strings.TrimSuffix(base, "/")
			if base == "" || rel == base || strings.HasPrefix(rel, base+"/") {
				return true
			}
		} else if rel == pat || pat == "" && rel == "." {
			return true
		}
	}
	return false
}

// gofmtOnly returns the source formatted by gofmt alone, or nil when it
// does not parse.
func gofmtOnly(src []byte) []byte {
	out, err := format.Source(src)
	if err != nil {
		return nil
	}
	return out
}

// formatFile formats like goimports. Formatting and import grouping need
// no go command; only a file that may miss an import or have an unused
// one goes through full goimports, which resolves packages (and starts the
// go command for each file).
func formatFile(src *diag.Sources, path string, data []byte) ([]byte, error) {
	if !mayNeedImports(data, dirNames(filepath.Dir(path))) {
		return imports.Process(path, data, &imports.Options{Comments: true, TabIndent: true, TabWidth: 8, FormatOnly: true})
	}
	return src.FormatFile(path, data)
}

// mayNeedImports reports whether goimports could add or remove an import:
// an import whose name no selector uses, or a selector on a name the file
// does not declare (it may be a package goimports would import).
func mayNeedImports(data []byte, pkgNames map[string]bool) bool {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", data, parser.ParseComments)
	if err != nil {
		return true
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok {
				used[x.Name] = true
			}
		}
		return true
	})
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		name := ""
		if im.Name != nil {
			name = im.Name.Name
		}
		if name == "_" || name == "." || path == "C" {
			continue
		}
		if name == "" {
			// The package name is usually the last element without a
			// version suffix or a go- prefix; when it is not, goimports
			// decides.
			name = importName(path)
		}
		if !used[name] {
			return true
		}
	}
	// Unresolved names used as selector bases: declared in another file of
	// the package, or a missing import.
	for _, id := range f.Unresolved {
		if !used[id.Name] || pkgNames[id.Name] {
			continue
		}
		imported := false
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if im.Name != nil && im.Name.Name == id.Name || im.Name == nil && importName(p) == id.Name {
				imported = true
			}
		}
		if !imported {
			return true
		}
	}
	return false
}

func importName(path string) string {
	name := path[strings.LastIndex(path, "/")+1:]
	if len(name) > 1 && name[0] == 'v' && strings.Trim(name[1:], "0123456789") == "" {
		// a major version suffix: example.com/x/v2
		if i := strings.LastIndex(path[:len(path)-len(name)-1], "/"); i >= 0 {
			name = path[i+1 : len(path)-len(name)-1]
		}
	}
	name = strings.TrimPrefix(name, "go-")
	if i := strings.IndexAny(name, ".-"); i >= 0 {
		name = name[:i]
	}
	return name
}

var dirNameCache sync.Map // dir -> *dirNamesEntry

type dirNamesEntry struct {
	once  sync.Once
	names map[string]bool
}

// dirNames returns the top level names declared in the Go files of a
// directory, of every package and build configuration in it.
func dirNames(dir string) map[string]bool {
	v, _ := dirNameCache.LoadOrStore(dir, &dirNamesEntry{})
	e := v.(*dirNamesEntry)
	e.once.Do(func() {
		e.names = map[string]bool{}
		ents, _ := os.ReadDir(dir)
		for _, de := range ents {
			if de.IsDir() || !strings.HasSuffix(de.Name(), ".go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, de.Name()), nil, parser.SkipObjectResolution)
			if err != nil {
				continue
			}
			for _, d := range f.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil {
						e.names[d.Name.Name] = true
					}
				case *ast.GenDecl:
					for _, sp := range d.Specs {
						switch sp := sp.(type) {
						case *ast.ValueSpec:
							for _, n := range sp.Names {
								e.names[n.Name] = true
							}
						case *ast.TypeSpec:
							e.names[sp.Name.Name] = true
						}
					}
				}
			}
		}
	})
	return e.names
}
