package diag

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Options control enrichment.
type Options struct {
	// Context fills Diagnostic.Context (agent mode).
	Context bool
	// ContextLines is the number of lines around the span in Context.
	ContextLines int
}

var (
	reUndefined   = lazy(`^undefined: ([\w.]+)$`)
	reNoField     = lazy(`^([\w.()\[\]*]+)\.(\w+) undefined \(type (\S+) has no field or method (\w+)(?:, but does have (?:field|method) (\w+))?\)`)
	reUnusedVar   = lazy(`^declared and not used: (\w+)$`)
	reUnusedImp   = lazy(`^"([^"]+)" imported(?: as (\w+))? and not used`)
	reCannotUse   = lazy(`^cannot use (.+) \((.+?)\) as (.+?) value in (argument to ([\w.()*]+)|assignment|return statement|variable declaration|struct literal|array or slice literal|map literal|send)`)
	reMismatch    = lazy(`^invalid operation: (.+) \(mismatched types (.+) and (.+)\)$`)
	reArgCount    = lazy(`^(?:too many|not enough) arguments in call to ([\w.()*]+)`)
	reAssignCount = lazy(`^assignment mismatch: (\d+) variables? but (.+) returns? (\d+) values?`)
	reNotExported = lazy(`^name (\w+) not exported by package (\w+)`)
	reTypeName    = lazy(`\b(?:\*?)((?:\w+\.)?[A-Za-z_]\w*)\b`)
)

// Enrich adds spans, labels, notes, help and fixes from the source files.
// It never fails: a diagnostic it cannot improve is left as it is.
func (s *Sources) Enrich(ds []Diagnostic, opt Options) {
	for i := range ds {
		s.enrich(&ds[i], opt)
	}
}

func (s *Sources) enrich(d *Diagnostic, opt Options) {
	if !d.Span.Valid() || !strings.HasSuffix(d.Span.File, ".go") {
		return
	}
	f := s.file(d.Span.File)
	if f == nil {
		return
	}
	if d.Span.Col == 0 {
		d.Span.Col = 1
	}
	if d.Span.EndLine == 0 {
		d.Span.EndLine, d.Span.EndCol = f.tokenEnd(d.Span.Line, d.Span.Col)
	}
	pos := f.tokenPos(d.Span.Line, d.Span.Col)
	switch d.Code {
	case CodeUndefined:
		s.undefined(d, f, pos)
	case CodeNoFieldMethod:
		s.noFieldMethod(d, f, pos)
	case CodeUnusedVar:
		s.unusedVar(d, f, pos)
	case CodeUnusedImport:
		s.unusedImport(d, f)
	case CodeCannotUse:
		s.cannotUse(d, f, pos)
	case CodeMismatched:
		s.mismatched(d, f, pos)
	case CodeReturnCount:
		s.returnCount(d, f, pos)
	case CodeArgCount:
		s.argCount(d, f, pos)
	case CodeAssignCount:
		s.assignCount(d, f, pos)
	case CodeNoNewVars:
		s.noNewVars(d, f, pos)
	case CodePkgNotFound:
		var keep []string
		for _, n := range d.Notes {
			if strings.HasPrefix(n, "go get ") {
				d.Help = append(d.Help, "add the module: "+n)
				continue
			}
			keep = append(keep, n)
		}
		d.Notes = keep
		d.Message = strings.TrimSuffix(d.Message, "; to add it:")
	case CodeTestFail:
		if t, ok := f.line(d.Span.Line); ok {
			ind := len(t) - len(strings.TrimLeft(t, " \t"))
			d.Span.Col, d.Span.EndLine, d.Span.EndCol = ind+1, d.Span.Line, len(strings.TrimRight(t, " \t"))+1
			d.Primary = "failed here"
		}
	case CodeSyntax:
		syntaxHint(d, f)
	case CodeMissingReturn:
		d.Help = append(d.Help, "add a return statement at the end of the function, or panic if the end cannot be reached")
	case CodeNotExported:
		if m := reNotExported.FindStringSubmatch(d.Message); m != nil {
			up := strings.ToUpper(m[1][:1]) + m[1][1:]
			d.Help = append(d.Help, fmt.Sprintf("only exported names (upper case first letter) of %s are visible here; did you mean %s.%s?", m[2], m[2], up))
		}
	}
	if opt.Context {
		s.context(d, f, pos, opt)
	}
}

// syntaxHint labels an unfinished expression on the line before an
// "expected operand" error.
func syntaxHint(d *Diagnostic, f *srcFile) {
	if !strings.HasPrefix(d.Message, "expected operand") {
		return
	}
	for l := d.Span.Line - 1; l > 0 && l >= d.Span.Line-3; l-- {
		t, _ := f.line(l)
		tr := strings.TrimRight(t, " \t")
		if tr == "" {
			continue
		}
		last := tr[len(tr)-1]
		if strings.IndexByte("+-*/%&|^<>=,(!", last) >= 0 {
			sp := Span{File: f.path, Line: l, Col: len(tr), EndLine: l, EndCol: len(tr) + 1}
			d.Labels = append(d.Labels, Label{Span: sp, Text: "the expression is not finished"})
			d.Primary = "an operand was expected here"
		}
		return
	}
}

// setSpanLen narrows the primary span to n bytes from its start.
func setSpanLen(d *Diagnostic, n int) {
	d.Span.EndLine, d.Span.EndCol = d.Span.Line, d.Span.Col+n
}

// at reports whether the source at the span start is s.
func (f *srcFile) at(sp Span, s string) bool {
	o := f.offset(sp.Line, sp.Col)
	return o >= 0 && o+len(s) <= len(f.data) && string(f.data[o:o+len(s)]) == s
}

func (s *Sources) undefined(d *Diagnostic, f *srcFile, pos token.Pos) {
	m := reUndefined.FindStringSubmatch(d.Message)
	if m == nil {
		return
	}
	name := m[1]
	if pkg, sel, ok := strings.Cut(name, "."); ok {
		// undefined: fmt.Printn. go/types points at the selector.
		if f.at(d.Span, sel) {
			setSpanLen(d, len(sel))
		} else if f.at(d.Span, name) {
			d.Span.Col += len(pkg) + 1
			setSpanLen(d, len(sel))
		}
		ex := s.importedExports(f, pkg)
		if ex == nil {
			return
		}
		d.Primary = "not declared by package " + pkg
		cands := make([]string, 0, len(ex.names))
		for n := range ex.names {
			cands = append(cands, n)
		}
		if best := closest(sel, cands); best != "" {
			d.Primary = "did you mean " + best + "?"
			d.Help = append(d.Help, fmt.Sprintf("a %s with a similar name exists: %s.%s", ex.names[best], pkg, best))
			d.Fixes = append(d.Fixes, Fix{Title: "use " + pkg + "." + best, Edits: []Edit{{File: f.path, Span: d.Span, NewText: best}}})
		}
		return
	}
	if f.at(d.Span, name) {
		setSpanLen(d, len(name))
	}
	// A package name used without its import?
	if f.ast != nil && isPkgUse(f, pos) {
		if path, unique := s.stdPkgFor(name, selectorAfter(f, d.Span)); path != "" {
			d.Primary = "package " + name + " is not imported"
			d.Help = append(d.Help, fmt.Sprintf("add the import: import %q", path))
			if unique {
				d.Fixes = append(d.Fixes, Fix{Title: fmt.Sprintf("import %q", path), Edits: []Edit{addImportEdit(f, path)}})
			}
			return
		}
	}
	// A misspelled package name?
	if f.ast != nil && isPkgUse(f, pos) {
		sel := selectorAfter(f, d.Span)
		var names []string
		for n := range s.stdIdx().byName {
			names = append(names, n)
		}
		if best := closest(name, names); best != "" {
			if path, _ := s.stdPkgFor(best, sel); path != "" {
				d.Primary = "did you mean " + best + "?"
				d.Help = append(d.Help, fmt.Sprintf("package %s (import %q) declares %s", best, path, sel))
				return
			}
		}
	}
	// Did you mean another name in scope?
	cands := s.scopeNames(f, pos)
	if best := closest(name, cands); best != "" {
		d.Primary = "did you mean " + best + "?"
		d.Fixes = append(d.Fixes, Fix{Title: "use " + best, Edits: []Edit{{File: f.path, Span: d.Span, NewText: best}}})
		return
	}
	d.Primary = "not declared in this scope"
}

// isPkgUse reports whether the identifier at pos is the X of a selector
// expression (name.Sel).
func isPkgUse(f *srcFile, pos token.Pos) bool {
	path := pathNodes(f.ast, pos)
	for i := len(path) - 1; i >= 0; i-- {
		if se, ok := path[i].(*ast.SelectorExpr); ok {
			id, isID := se.X.(*ast.Ident)
			return isID && id.Pos() == pos
		}
		if _, ok := path[i].(*ast.Ident); !ok {
			return false
		}
	}
	return false
}

// selectorAfter returns Sel when the span is followed by .Sel.
func selectorAfter(f *srcFile, sp Span) string {
	o := f.offset(sp.EndLine, sp.EndCol)
	if o < 0 || o >= len(f.data) || f.data[o] != '.' {
		return ""
	}
	j := o + 1
	for j < len(f.data) && isIdentByte(f.data[j]) {
		j++
	}
	return string(f.data[o+1 : j])
}

// stdPkgFor finds the standard library package named name that declares sel.
// unique is false when several packages fit.
func (s *Sources) stdPkgFor(name, sel string) (path string, unique bool) {
	paths := s.stdIdx().byName[name]
	var fit []string
	for _, p := range paths {
		if sel == "" {
			fit = append(fit, p)
			continue
		}
		if ex := s.stdExports(p); ex != nil && ex.names[sel] != "" {
			fit = append(fit, p)
		}
	}
	if len(fit) == 0 {
		return "", false
	}
	if pref, ok := preferredStd[name]; ok {
		for _, p := range fit {
			if p == pref {
				return p, true
			}
		}
	}
	return fit[0], len(fit) == 1
}

// preferredStd picks between standard packages with the same name.
var preferredStd = map[string]string{
	"rand":     "math/rand/v2",
	"template": "text/template",
	"scanner":  "text/scanner",
	"pprof":    "runtime/pprof",
}

// addImportEdit inserts an import of path in the file.
func addImportEdit(f *srcFile, path string) Edit {
	quoted := strconv.Quote(path)
	for _, d := range f.ast.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		if gd.Lparen.IsValid() {
			// Insert in sorted position among the specs of the group.
			for _, sp := range gd.Specs {
				is := sp.(*ast.ImportSpec)
				if is.Path.Value > quoted && is.Name == nil {
					p := f.fset.Position(is.Pos())
					sp := Span{File: f.path, Line: p.Line, Col: 1, EndLine: p.Line, EndCol: 1}
					return Edit{File: f.path, Span: sp, NewText: "\t" + quoted + "\n"}
				}
			}
			p := f.fset.Position(gd.Rparen)
			sp := Span{File: f.path, Line: p.Line, Col: 1, EndLine: p.Line, EndCol: 1}
			return Edit{File: f.path, Span: sp, NewText: "\t" + quoted + "\n"}
		}
		// import "x": add a second import line after it.
		p := f.fset.Position(gd.End())
		sp := Span{File: f.path, Line: p.Line + 1, Col: 1, EndLine: p.Line + 1, EndCol: 1}
		return Edit{File: f.path, Span: sp, NewText: "import " + quoted + "\n"}
	}
	p := f.fset.Position(f.ast.Name.End())
	sp := Span{File: f.path, Line: p.Line + 1, Col: 1, EndLine: p.Line + 1, EndCol: 1}
	return Edit{File: f.path, Span: sp, NewText: "\nimport " + quoted + "\n"}
}

// importFor returns the import spec of the file that binds name.
func (s *Sources) importFor(f *srcFile, name string) (path string, ok bool) {
	if f.ast == nil {
		return "", false
	}
	for _, is := range f.ast.Imports {
		p, err := strconv.Unquote(is.Path.Value)
		if err != nil {
			continue
		}
		bound := ""
		if is.Name != nil {
			bound = is.Name.Name
		} else {
			bound = s.pkgNameOf(p)
		}
		if bound == name {
			return p, true
		}
	}
	return "", false
}

// pkgNameOf guesses the package name of an import path: the last element,
// skipping a major version suffix.
func (s *Sources) pkgNameOf(path string) string {
	elems := strings.Split(path, "/")
	last := elems[len(elems)-1]
	if len(elems) > 1 && len(last) > 1 && last[0] == 'v' && last[1] >= '0' && last[1] <= '9' {
		last = elems[len(elems)-2]
	}
	last = strings.TrimPrefix(last, "go-")
	if i := strings.IndexAny(last, ".-"); i > 0 {
		last = last[:i]
	}
	return last
}

// importedExports returns the exports of the package the file imports as
// name, from the standard library or the main module.
func (s *Sources) importedExports(f *srcFile, name string) *exports {
	path, ok := s.importFor(f, name)
	if !ok {
		return nil
	}
	if dir := s.pkgDir(f.path, path); dir != "" {
		return pkgExports(dir, false)
	}
	return nil
}

// pkgDir resolves an import path to a directory: standard library, or a
// package of the module that contains from.
func (s *Sources) pkgDir(from, path string) string {
	first, _, _ := strings.Cut(path, "/")
	if !strings.Contains(first, ".") {
		if ix := s.stdIdx(); ix.root != "" {
			d := filepath.Join(ix.root, filepath.FromSlash(path))
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				return d
			}
		}
	}
	modRoot, modPath := findModule(filepath.Dir(from))
	if modRoot == "" {
		return ""
	}
	if path == modPath {
		return modRoot
	}
	if rest, ok := strings.CutPrefix(path, modPath+"/"); ok {
		return filepath.Join(modRoot, filepath.FromSlash(rest))
	}
	return ""
}

func findModule(dir string) (root, path string) {
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			for _, l := range strings.Split(string(data), "\n") {
				l = strings.TrimSpace(l)
				if rest, ok := strings.CutPrefix(l, "module "); ok {
					return dir, strings.Trim(strings.TrimSpace(rest), `"`)
				}
			}
			return dir, ""
		}
		up := filepath.Dir(dir)
		if up == dir {
			return "", ""
		}
		dir = up
	}
}

// universe holds the predeclared names.
var universe = strings.Fields(`append cap clear close complex copy delete imag len make max min new panic print println real recover
	bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any
	true false iota nil`)

// scopeNames lists names visible at pos: universe, package level
// declarations, imports, and the locals and parameters of the enclosing
// function declared before pos.
func (s *Sources) scopeNames(f *srcFile, pos token.Pos) []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		if n != "_" && n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, u := range universe {
		add(u)
	}
	pkg := s.pkgOf(f.path)
	for _, pf := range pkg.files {
		for _, d := range pf.ast.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					add(d.Name.Name)
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.TypeSpec:
						add(sp.Name.Name)
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							add(n.Name)
						}
					}
				}
			}
		}
	}
	if f.ast != nil {
		for _, is := range f.ast.Imports {
			if p, err := strconv.Unquote(is.Path.Value); err == nil {
				if is.Name != nil {
					add(is.Name.Name)
				} else {
					add(s.pkgNameOf(p))
				}
			}
		}
	}
	if fd := enclosingFunc(f, pos); fd != nil {
		ast.Inspect(fd, func(n ast.Node) bool {
			if n == nil || n.Pos() >= pos {
				return n == nil || n.Pos() < pos
			}
			switch n := n.(type) {
			case *ast.Field:
				for _, nm := range n.Names {
					add(nm.Name)
				}
			case *ast.AssignStmt:
				if n.Tok == token.DEFINE {
					for _, l := range n.Lhs {
						if id, ok := l.(*ast.Ident); ok {
							add(id.Name)
						}
					}
				}
			case *ast.ValueSpec:
				for _, nm := range n.Names {
					add(nm.Name)
				}
			case *ast.RangeStmt:
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if id, ok := e.(*ast.Ident); ok {
						add(id.Name)
					}
				}
			}
			return true
		})
	}
	return out
}

// closest returns the candidate nearest to name by edit distance, or "".
// A candidate that differs only in case always wins.
func closest(name string, cands []string) string {
	best, bestD := "", 1<<30
	limit := max(1, (len(name)+2)/3)
	sort.Strings(cands)
	for _, c := range cands {
		if c == name {
			return "" // the name exists; a suggestion would be noise
		}
		if strings.EqualFold(c, name) {
			return c
		}
		if abs(len(c)-len(name)) > limit {
			continue
		}
		d := editDistance(name, c)
		if d > limit {
			continue
		}
		// Ties go to the candidate sharing the longer prefix and suffix.
		if d < bestD || d == bestD && affix(name, c) > affix(name, best) {
			best, bestD = c, d
		}
	}
	return best
}

// affix is the length of the common prefix plus the common suffix.
func affix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	m := 0
	for m < len(a)-n && m < len(b)-n && a[len(a)-1-m] == b[len(b)-1-m] {
		m++
	}
	return n + m
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// editDistance is the Damerau-Levenshtein distance (with adjacent swaps).
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev2 := make([]int, len(rb)+1)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(rb)]
}

func (s *Sources) noFieldMethod(d *Diagnostic, f *srcFile, pos token.Pos) {
	m := reNoField.FindStringSubmatch(d.Message)
	if m == nil {
		return
	}
	sel, typ := m[4], m[3]
	// The position is at the start of the selector expression; point at Sel.
	if f.ast != nil {
		for _, n := range pathNodes(f.ast, pos) {
			if se, ok := n.(*ast.SelectorExpr); ok && se.Sel.Name == sel && se.Pos() == pos {
				d.Span = f.spanOf(se.Sel)
			}
		}
	}
	if m[5] != "" {
		d.Primary = "did you mean " + m[5] + "?"
		d.Fixes = append(d.Fixes, Fix{Title: "use " + m[5], Edits: []Edit{{File: f.path, Span: d.Span, NewText: m[5]}}})
		return
	}
	base := strings.TrimPrefix(typ, "*")
	ex := s.typeExports(f, base)
	if ex == nil {
		d.Primary = "unknown field or method"
		return
	}
	tname := base
	if _, after, ok := strings.Cut(base, "."); ok {
		tname = after
	}
	var cands []string
	var set []string
	for mname, recv := range ex.methods[tname] {
		cands = append(cands, mname)
		if recv == "*T" {
			set = append(set, "(*"+tname+") "+mname)
		} else {
			set = append(set, "("+tname+") "+mname)
		}
	}
	cands = append(cands, ex.fields[tname]...)
	sort.Strings(set)
	if best := closest(sel, cands); best != "" {
		d.Primary = "did you mean " + best + "?"
		d.Fixes = append(d.Fixes, Fix{Title: "use " + best, Edits: []Edit{{File: f.path, Span: d.Span, NewText: best}}})
	} else {
		d.Primary = "no field or method " + sel
	}
	if fl := ex.fields[tname]; len(fl) > 0 {
		d.Notes = append(d.Notes, fmt.Sprintf("fields of %s: %s", base, strings.Join(fl, ", ")))
	}
	if len(set) > 0 {
		if len(set) > 12 {
			set = append(set[:12], fmt.Sprintf("and %d more", len(set)-12))
		}
		d.Notes = append(d.Notes, fmt.Sprintf("methods of %s: %s", base, strings.Join(set, ", ")))
		if strings.HasPrefix(typ, "*") == false {
			for _, x := range set {
				if strings.HasSuffix(x, ") "+sel) && strings.HasPrefix(x, "(*") {
					d.Help = append(d.Help, sel+" has a pointer receiver: call it on a pointer or an addressable value")
				}
			}
		}
	}
}

// typeExports finds the declarations of a named type: T in the package of
// f, or pkg.T in an imported package.
func (s *Sources) typeExports(f *srcFile, typ string) *exports {
	if pkg, _, ok := strings.Cut(typ, "."); ok {
		if p := s.pkgOf(f.path); p.name == pkg {
			return s.localExports(f)
		}
		// Message types are qualified by package name, the file may not
		// import it under that name (types from dependencies).
		if ex := s.importedExports(f, pkg); ex != nil {
			return ex
		}
		if path, _ := s.stdPkgFor(pkg, ""); path != "" {
			return s.stdExports(path)
		}
		return nil
	}
	return s.localExports(f)
}

func (s *Sources) localExports(f *srcFile) *exports {
	ex := &exports{names: map[string]string{}, methods: map[string]map[string]string{}, fields: map[string][]string{}, decl: map[string]string{}}
	for _, pf := range s.pkgOf(f.path).files {
		collectExports(ex, pf.fset, pf.data, pf.ast, true)
	}
	return ex
}

func (s *Sources) unusedVar(d *Diagnostic, f *srcFile, pos token.Pos) {
	m := reUnusedVar.FindStringSubmatch(d.Message)
	if m == nil || f.ast == nil {
		return
	}
	name := m[1]
	if f.at(d.Span, name) {
		setSpanLen(d, len(name))
	}
	d.Primary = "declared here, never used"
	path := pathNodes(f.ast, pos)
	for i := len(path) - 1; i >= 0; i-- {
		switch n := path[i].(type) {
		case *ast.AssignStmt:
			if n.Tok != token.DEFINE {
				return
			}
			if len(n.Lhs) == 1 && len(n.Rhs) == 1 && pure(n.Rhs[0]) {
				if e, ok := deleteLines(f, n); ok {
					d.Fixes = append(d.Fixes, Fix{Title: "remove " + name, Edits: []Edit{e}})
					return
				}
			}
			if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
				sp := f.spanOf(n.Lhs[0])
				tp := f.fset.Position(n.TokPos)
				sp.EndLine, sp.EndCol = tp.Line, tp.Column+2
				d.Fixes = append(d.Fixes, Fix{Title: "assign the value to _", Edits: []Edit{{File: f.path, Span: sp, NewText: "_ ="}}})
				d.Help = append(d.Help, "remove the statement if the value is not needed")
				return
			}
			others := 0
			for _, l := range n.Lhs {
				if id, ok := l.(*ast.Ident); ok && id.Name != "_" && id.Name != name {
					others++
				}
			}
			if others > 0 {
				d.Fixes = append(d.Fixes, Fix{Title: "replace " + name + " with _", Edits: []Edit{{File: f.path, Span: d.Span, NewText: "_"}}})
			}
			return
		case *ast.RangeStmt:
			if id, ok := n.Value.(*ast.Ident); ok && id.Pos() == pos {
				// for k, v := range: drop ", v".
				ke := f.fset.Position(n.Key.End())
				sp := Span{File: f.path, Line: ke.Line, Col: ke.Column, EndLine: d.Span.EndLine, EndCol: d.Span.EndCol}
				d.Fixes = append(d.Fixes, Fix{Title: "drop " + name, Edits: []Edit{{File: f.path, Span: sp, NewText: ""}}})
				return
			}
			if id, ok := n.Key.(*ast.Ident); ok && id.Pos() == pos {
				if n.Value == nil {
					// for i := range xs: drop "i := ".
					sp := f.spanOf(n.Key)
					tp := f.fset.Position(n.TokPos)
					sp.EndLine, sp.EndCol = tp.Line, tp.Column+3
					d.Fixes = append(d.Fixes, Fix{Title: "drop " + name, Edits: []Edit{{File: f.path, Span: sp, NewText: ""}}})
				} else {
					d.Fixes = append(d.Fixes, Fix{Title: "replace " + name + " with _", Edits: []Edit{{File: f.path, Span: d.Span, NewText: "_"}}})
				}
			}
			return
		case *ast.DeclStmt:
			gd := n.Decl.(*ast.GenDecl)
			if len(gd.Specs) == 1 && len(gd.Specs[0].(*ast.ValueSpec).Names) == 1 {
				if e, ok := deleteLines(f, n); ok {
					d.Fixes = append(d.Fixes, Fix{Title: "remove the declaration of " + name, Edits: []Edit{e}})
				}
			}
			return
		}
	}
}

// pure reports whether evaluating e has no side effects and cannot panic,
// so a statement that only assigns it can be removed.
func pure(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.BasicLit, *ast.Ident, *ast.FuncLit:
		return true
	case *ast.ParenExpr:
		return pure(e.X)
	case *ast.UnaryExpr:
		return e.Op != token.ARROW && pure(e.X)
	case *ast.BinaryExpr:
		return e.Op != token.QUO && e.Op != token.REM && pure(e.X) && pure(e.Y)
	case *ast.CompositeLit:
		for _, x := range e.Elts {
			if kv, ok := x.(*ast.KeyValueExpr); ok {
				x = kv.Value
			}
			if !pure(x) {
				return false
			}
		}
		return true
	}
	return false
}

// deleteLines returns an edit that removes the whole lines n occupies, when
// nothing else is on them.
func deleteLines(f *srcFile, n ast.Node) (Edit, bool) {
	p, e := f.fset.Position(n.Pos()), f.fset.Position(n.End())
	first, _ := f.line(p.Line)
	last, _ := f.line(e.Line)
	if strings.TrimSpace(first[:p.Column-1]) != "" || strings.TrimSpace(last[e.Column-1:]) != "" {
		return Edit{}, false
	}
	sp := Span{File: f.path, Line: p.Line, Col: 1, EndLine: e.Line + 1, EndCol: 1}
	return Edit{File: f.path, Span: sp, NewText: ""}, true
}

func (s *Sources) unusedImport(d *Diagnostic, f *srcFile) {
	m := reUnusedImp.FindStringSubmatch(d.Message)
	if m == nil || f.ast == nil {
		return
	}
	path := m[1]
	d.Primary = "not used in this file"
	for _, dl := range f.ast.Decls {
		gd, ok := dl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, sp := range gd.Specs {
			is := sp.(*ast.ImportSpec)
			if p, _ := strconv.Unquote(is.Path.Value); p != path {
				continue
			}
			d.Span = f.spanOf(is)
			var node ast.Node = is
			if len(gd.Specs) == 1 {
				node = gd
			}
			if e, ok := deleteLines(f, node); ok {
				d.Fixes = append(d.Fixes, Fix{Title: fmt.Sprintf("remove the import of %q", path), Edits: []Edit{e}})
			}
			d.Help = append(d.Help, fmt.Sprintf("if the package is imported only for its side effects, write import _ %q", path))
			return
		}
	}
}

func (s *Sources) cannotUse(d *Diagnostic, f *srcFile, pos token.Pos) {
	m := reCannotUse.FindStringSubmatch(d.Message)
	if m == nil {
		return
	}
	expr, have, want, callee := m[1], m[2], m[3], m[5]
	if f.ast != nil {
		for _, n := range pathNodes(f.ast, pos) {
			if e, ok := n.(ast.Expr); ok && e.Pos() == pos && f.text(e) == expr {
				d.Span = f.spanOf(e)
				break
			}
		}
	}
	haveT := have
	if i := strings.LastIndex(have, " of type "); i >= 0 {
		haveT = have[i+len(" of type "):]
	} else if strings.HasPrefix(have, "untyped ") {
		haveT = strings.TrimSuffix(strings.TrimSuffix(have, " constant"), " value")
	}
	d.Primary = fmt.Sprintf("this is %s, want %s", haveT, want)
	if callee != "" {
		s.relatedFunc(d, f, callee)
	}
	if h := conversionHelp(expr, haveT, want); h != "" {
		d.Help = append(d.Help, h)
	}
}

var numeric = map[string]bool{"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true,
	"float32": true, "float64": true, "byte": true, "rune": true,
	"untyped int": true, "untyped float": true, "untyped rune": true}

// conversionHelp suggests a conversion between two types.
func conversionHelp(expr, have, want string) string {
	isStr := func(t string) bool { return t == "string" || t == "untyped string" }
	isInt := func(t string) bool { return numeric[t] && !strings.Contains(t, "float") }
	switch {
	case numeric[have] && numeric[want]:
		return fmt.Sprintf("convert the value: %s(%s)", want, expr)
	case isStr(have) && isInt(want):
		return fmt.Sprintf("to parse a number from a string, use strconv.Atoi(%s)", expr)
	case isInt(have) && isStr(want):
		return fmt.Sprintf("to format a number as text, use strconv.Itoa(%s); string(%s) gives the character with that code point", expr, expr)
	case have == "[]byte" && isStr(want):
		return fmt.Sprintf("convert the bytes: string(%s)", expr)
	case isStr(have) && want == "[]byte":
		return fmt.Sprintf("convert the string: []byte(%s)", expr)
	}
	return ""
}

// relatedFunc adds the declaration of the named function as related.
func (s *Sources) relatedFunc(d *Diagnostic, f *srcFile, name string) {
	name = strings.TrimPrefix(name, "(")
	// Method expressions and pkg.F: use the last element.
	if i := strings.LastIndex(name, "."); i >= 0 {
		pkg := name[:i]
		fn := name[i+1:]
		if ex := s.importedExports(f, pkg); ex != nil {
			_ = ex
			if sig := s.importedFuncSig(f, pkg, fn); sig != "" {
				d.Notes = append(d.Notes, fmt.Sprintf("%s.%s is declared as %s", pkg, fn, sig))
			}
			return
		}
		name = fn
	}
	for _, pf := range s.pkgOf(f.path).files {
		for _, dl := range pf.ast.Decls {
			if fd, ok := dl.(*ast.FuncDecl); ok && fd.Name.Name == name {
				sig := signature(pf, fd)
				d.Related = append(d.Related, Related{Span: pf.spanOf(fd.Name), Message: name + " is declared here"})
				d.Notes = append(d.Notes, "signature: "+sig)
				return
			}
		}
	}
}

// funcSig returns the signature of a function named name or pkg.name.
func (s *Sources) funcSig(f *srcFile, name string) string {
	if pkg, fn, ok := strings.Cut(name, "."); ok {
		return s.importedFuncSig(f, pkg, fn)
	}
	for _, pf := range s.pkgOf(f.path).files {
		for _, dl := range pf.ast.Decls {
			if fd, ok := dl.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
				return signature(pf, fd)
			}
		}
	}
	return ""
}

// importedFuncSig returns the signature of pkg.fn from its source.
func (s *Sources) importedFuncSig(f *srcFile, pkg, fn string) string {
	path, ok := s.importFor(f, pkg)
	if !ok {
		return ""
	}
	dir := s.pkgDir(f.path, path)
	if dir == "" {
		return ""
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		pf := s.file(filepath.Join(dir, n))
		if pf == nil || pf.ast == nil {
			continue
		}
		for _, dl := range pf.ast.Decls {
			if fd, ok := dl.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == fn {
				return signature(pf, fd)
			}
		}
	}
	return ""
}

func (s *Sources) mismatched(d *Diagnostic, f *srcFile, pos token.Pos) {
	m := reMismatch.FindStringSubmatch(d.Message)
	if m == nil || f.ast == nil {
		return
	}
	lt, rt := m[2], m[3]
	for _, n := range pathNodes(f.ast, pos) {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || f.text(be) != m[1] {
			continue
		}
		op := f.fset.Position(be.OpPos)
		d.Span = Span{File: f.path, Line: op.Line, Col: op.Column, EndLine: op.Line, EndCol: op.Column + len(be.Op.String())}
		d.Primary = ""
		d.Labels = append(d.Labels, Label{Span: f.spanOf(be.X), Text: lt}, Label{Span: f.spanOf(be.Y), Text: rt})
		if numeric[lt] && numeric[rt] {
			// Convert toward floating point so no fraction is lost.
			if strings.HasPrefix(rt, "float") && !strings.HasPrefix(lt, "float") {
				d.Help = append(d.Help, fmt.Sprintf("convert one operand, for example %s(%s) %s %s", rt, f.text(be.X), be.Op, f.text(be.Y)))
			} else {
				d.Help = append(d.Help, fmt.Sprintf("convert one operand, for example %s %s %s(%s)", f.text(be.X), be.Op, lt, f.text(be.Y)))
			}
		}
		return
	}
}

func (s *Sources) returnCount(d *Diagnostic, f *srcFile, pos token.Pos) {
	if f.ast == nil {
		return
	}
	for _, n := range pathNodes(f.ast, pos) {
		if rs, ok := n.(*ast.ReturnStmt); ok && len(rs.Results) > 0 {
			first, last := rs.Results[0], rs.Results[len(rs.Results)-1]
			a, b := f.spanOf(first), f.spanOf(last)
			d.Span = Span{File: f.path, Line: a.Line, Col: a.Col, EndLine: b.EndLine, EndCol: b.EndCol}
		}
	}
	fd := enclosingFunc(f, pos)
	if fd == nil {
		return
	}
	var lit *ast.FuncLit
	for _, n := range pathNodes(f.ast, pos) {
		if fl, ok := n.(*ast.FuncLit); ok {
			lit = fl
		}
	}
	if lit != nil {
		d.Labels = append(d.Labels, Label{Span: f.spanOf(lit.Type), Text: "the function literal's signature"})
		return
	}
	d.Primary = "returned here"
	sig := signature(f, fd)
	d.Related = append(d.Related, Related{Span: f.spanOf(fd.Name), Message: "signature: " + sig})
	if fd.Type.Results == nil || len(fd.Type.Results.List) == 0 {
		d.Help = append(d.Help, fmt.Sprintf("%s has no results; to return a value, declare it: %s T", fd.Name.Name, oneLine(f.text(fd.Type))))
	} else {
		d.Help = append(d.Help, "the signature is "+sig)
	}
}

func (s *Sources) argCount(d *Diagnostic, f *srcFile, pos token.Pos) {
	m := reArgCount.FindStringSubmatch(d.Message)
	if m == nil {
		return
	}
	s.relatedFunc(d, f, m[1])
}

func (s *Sources) assignCount(d *Diagnostic, f *srcFile, pos token.Pos) {
	m := reAssignCount.FindStringSubmatch(d.Message)
	if m == nil || f.ast == nil {
		return
	}
	nv, _ := strconv.Atoi(m[1])
	nr, _ := strconv.Atoi(m[3])
	for _, n := range pathNodes(f.ast, pos) {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != nv || nr <= nv {
			continue
		}
		var names []string
		for _, l := range as.Lhs {
			names = append(names, f.text(l))
		}
		for len(names) < nr {
			if sig := s.funcSig(f, m[2]); len(names) == nr-1 && (strings.HasSuffix(sig, " error") || strings.HasSuffix(sig, " error)")) {
				names = append(names, "err")
			} else {
				names = append(names, "_")
			}
		}
		d.Help = append(d.Help, fmt.Sprintf("%s returns %d values; receive all of them: %s %s %s", m[2], nr, strings.Join(names, ", "), as.Tok, f.text(as.Rhs[0])))
		return
	}
}

func (s *Sources) noNewVars(d *Diagnostic, f *srcFile, pos token.Pos) {
	if f.ast == nil {
		return
	}
	for _, n := range pathNodes(f.ast, pos) {
		if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
			p := f.fset.Position(as.TokPos)
			sp := Span{File: f.path, Line: p.Line, Col: p.Column, EndLine: p.Line, EndCol: p.Column + 2}
			d.Span = sp
			d.Primary = "every name on the left is already declared"
			d.Fixes = append(d.Fixes, Fix{Title: "use = instead of :=", Edits: []Edit{{File: f.path, Span: sp, NewText: "="}}})
			return
		}
	}
}

// context fills the agent context: enclosing function, source lines, and
// one line definitions of the types the message names.
func (s *Sources) context(d *Diagnostic, f *srcFile, pos token.Pos, opt Options) {
	c := &Context{}
	if fd := enclosingFunc(f, pos); fd != nil {
		c.Function = signature(f, fd)
	}
	n := opt.ContextLines
	if n == 0 {
		n = 2
	}
	endLine := max(d.Span.EndLine, d.Span.Line)
	for l := max(1, d.Span.Line-n); l <= endLine+n; l++ {
		t, ok := f.line(l)
		if !ok {
			break
		}
		c.Lines = append(c.Lines, SourceLine{Line: l, Text: t})
	}
	seen := map[string]bool{}
	local := (*exports)(nil)
	for _, m := range reTypeName.FindAllStringSubmatch(d.Message, -1) {
		name := m[1]
		if seen[name] || numeric[name] || name == "string" || name == "bool" || name == "error" {
			continue
		}
		seen[name] = true
		if pkg, tn, ok := strings.Cut(name, "."); ok {
			if ex := s.typeExports(f, name); ex != nil {
				if def := ex.decl[tn]; def != "" {
					c.Types = append(c.Types, pkg+": "+def)
				}
			}
			continue
		}
		if local == nil {
			local = s.localExports(f)
		}
		if def := local.decl[name]; def != "" {
			c.Types = append(c.Types, def)
		}
	}
	d.Context = c
}
