package diag

import (
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

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
		if t, ok := f.line(d.Span.Line); ok && d.Span.Col > len(t) {
			// At the end of the line: one column, not the next token.
			d.Span.EndLine, d.Span.EndCol = d.Span.Line, d.Span.Col+1
		} else {
			d.Span.EndLine, d.Span.EndCol = f.tokenEnd(d.Span.Line, d.Span.Col)
		}
	}
	pos := f.tokenPos(d.Span.Line, d.Span.Col)
	haveWant(d)
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
		if strings.Contains(d.Message, " does not implement ") {
			d.Code = CodeNotImplement
			s.notImplement(d, f, pos)
			break
		}
		s.cannotUse(d, f, pos)
	case CodeNotImplement:
		s.notImplement(d, f, pos)
	case CodeNotSatisfy:
		s.notSatisfy(d, f, pos)
	case CodeCannotInfer:
		d.Primary = "cannot infer the type arguments of this call"
		if m := reDeclaredAt.FindStringSubmatch(d.Message); m != nil {
			l, _ := strconv.Atoi(m[2])
			c, _ := strconv.Atoi(m[3])
			if filepath.Base(f.path) == filepath.Base(m[1]) {
				el, ec := f.tokenEnd(l, c)
				d.Labels = append(d.Labels, Label{Span: Span{File: f.path, Line: l, Col: c, EndLine: el, EndCol: ec}, Text: "type parameter declared here"})
			}
		}
		if i := strings.Index(d.Message, "cannot infer "); i >= 0 {
			tp := strings.Fields(d.Message[i+len("cannot infer "):])
			if len(tp) > 0 && f.at(d.Span, "") {
				d.Help = append(d.Help, "give the type argument: `"+selectorOrName(f, d.Span)+"[T]()`, with the type you want for `"+tp[0]+"`")
			}
		}
	case CodeInvalidOp:
		s.invalidOp(d, f, pos)
	case CodeOther:
		s.otherHints(d, f, pos)
		if strings.Contains(d.Message, "is pointer to interface, not interface") {
			d.Code = CodeNoFieldMethod
			d.Primary = "a pointer to an interface has no methods"
			d.Help = append(d.Help, "use the interface type itself: an interface value already holds a pointer when the dynamic type is one")
		}
	case CodeMismatched:
		s.mismatched(d, f, pos)
	case CodeReturnCount:
		s.returnCount(d, f, pos)
	case CodeArgCount:
		s.argCount(d, f, pos)
	case CodeAssignCount:
		if strings.HasPrefix(d.Message, "multiple-value ") && f.ast != nil {
			for _, n := range pathNodes(f.ast, pos) {
				if ce, ok := n.(*ast.CallExpr); ok && ce.Pos() == pos {
					d.Span = f.spanOf(ce)
					d.Primary = "returns more than one value"
					name := f.text(ce.Fun)
					if sig := s.funcSig(f, name); sig != "" {
						d.Notes = append(d.Notes, fmt.Sprintf("`%s` is declared as `%s`", name, sig))
					}
					d.Help = append(d.Help, fmt.Sprintf("receive every result in a statement of its own, for example `v, err := %s`, and use `v` here", f.text(ce)))
				}
			}
		}
		s.assignCount(d, f, pos)
	case CodeNoNewVars:
		s.noNewVars(d, f, pos)
	case CodePkgNotFound:
		var keep []string
		for _, n := range d.Notes {
			if strings.HasPrefix(n, "go get ") {
				d.Help = append(d.Help, "add the module: `"+n+"`")
				continue
			}
			keep = append(keep, n)
		}
		d.Notes = keep
		d.Message = strings.TrimSuffix(d.Message, "; to add it:")
	case CodeHeapAlloc:
		if f.ast != nil {
			var best ast.Node
			path := pathNodes(f.ast, pos)
			box := strings.Contains(d.Message, ": box of ")
			for i, n := range path {
				if box {
					// A conversion to an interface: the argument converted.
					if call, ok := n.(*ast.CallExpr); ok && i+1 < len(path) {
						for _, a := range call.Args {
							if a == path[i+1] {
								best = a
							}
						}
					}
					continue
				}
				switch x := n.(type) {
				case *ast.CompositeLit, *ast.CallExpr, *ast.FuncLit:
					if f.fset.Position(x.Pos()).Line == d.Span.Line {
						best = x
					}
				case *ast.UnaryExpr:
					if x.Op == token.AND && f.fset.Position(x.Pos()).Line == d.Span.Line {
						best = x
					}
				}
			}
			if best != nil {
				sp := f.spanOf(best)
				if sp.EndLine != sp.Line {
					sp.EndLine, sp.EndCol = sp.Line, sp.Col+1
					if t, ok := f.line(sp.Line); ok {
						sp.EndCol = len(strings.TrimRight(t, " \t{")) + 1
					}
				}
				d.Span = sp
			}
		}
	case CodeTestFail:
		if t, ok := f.line(d.Span.Line); ok {
			ind := len(t) - len(strings.TrimLeft(t, " \t"))
			d.Span.Col, d.Span.EndLine, d.Span.EndCol = ind+1, d.Span.Line, len(strings.TrimRight(t, " \t"))+1
			d.Primary = "failed here"
		}
	case CodeSyntax:
		syntaxHint(d, f)
	case CodeMissingReturn:
		d.Help = append(d.Help, "add a `return` at the end of the function, or `panic` if the end cannot be reached")
	case CodeNotExported:
		if m := reNotExported.FindStringSubmatch(d.Message); m != nil {
			if f.at(d.Span, m[1]) {
				setSpanLen(d, len(m[1]))
			} else if f.at(d.Span, m[2]+"."+m[1]) {
				d.Span.Col += len(m[2]) + 1
				setSpanLen(d, len(m[1]))
			}
			d.Primary = "unexported: only package `" + m[2] + "` can use it"
			up := strings.ToUpper(m[1][:1]) + m[1][1:]
			if ex := s.importedExports(f, m[2]); ex != nil {
				if ex.names[up] != "" {
					d.Fixes = append(d.Fixes, Fix{Title: "use `" + m[2] + "." + up + "`", Applicability: MaybeIncorrect,
						Edits: []Edit{{File: f.path, Span: d.Span, NewText: up}}})
				} else if note := exportsNote(m[2], ex); note != "" {
					d.Notes = append(d.Notes, note)
				}
			}
		}
	case CodeUnknownField:
		s.unknownField(d, f, pos)
	}
	if opt.Context {
		s.context(d, f, pos, opt)
	}
}

// unclosedBrace finds the innermost { that is never closed.
func unclosedBrace(f *srcFile) (Span, bool) {
	var sc scanner.Scanner
	fs := token.NewFileSet()
	tf := fs.AddFile("", -1, len(f.data))
	sc.Init(tf, f.data, func(token.Position, string) {}, 0)
	var stack []int
	for {
		pos, tok, _ := sc.Scan()
		if tok == token.EOF {
			break
		}
		switch tok {
		case token.LBRACE:
			stack = append(stack, tf.Offset(pos))
		case token.RBRACE:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(stack) == 0 {
		return Span{}, false
	}
	l, c := f.pos(stack[len(stack)-1])
	return Span{File: f.path, Line: l, Col: c, EndLine: l, EndCol: c + 1}, true
}

// selectorOrName returns the identifier or qualified name at the span.
func selectorOrName(f *srcFile, sp Span) string {
	_, ec := f.tokenEnd(sp.Line, sp.Col)
	t, _ := f.line(sp.Line)
	if sp.Col-1 < ec-1 && ec-1 <= len(t) {
		return t[sp.Col-1 : ec-1]
	}
	return "f"
}

// syntaxHint explains the common syntax errors: an unfinished expression,
// a missing comma or closing bracket, an unclosed brace, else on its own
// line.
func syntaxHint(d *Diagnostic, f *srcFile) {
	switch {
	case strings.HasPrefix(d.Message, "missing ',' before newline in argument list"):
		d.Primary = "expected `,` or `)` here"
		d.Help = append(d.Help, "close the call with `)`, or put a `,` at the end of the line to continue the arguments")
		return
	case strings.HasPrefix(d.Message, "missing ',' before newline in composite literal"):
		d.Primary = "expected `,` here"
		d.Fixes = append(d.Fixes, Fix{Title: "add `,` (every element of a multi-line literal ends with one)", Applicability: MachineApplicable,
			Edits: []Edit{{File: f.path, Span: Span{File: f.path, Line: d.Span.Line, Col: d.Span.Col, EndLine: d.Span.Line, EndCol: d.Span.Col}, NewText: ","}}})
		return
	case strings.Contains(d.Message, "found 'EOF'"):
		if open, ok := unclosedBrace(f); ok {
			d.Primary = "the file ends here"
			d.Labels = append(d.Labels, Label{Span: open, Text: "this `{` is never closed"})
		}
		return
	case d.Message == "expected statement, found 'else'":
		d.Primary = "`else` must follow the `}` of the `if` on the same line"
		for l := d.Span.Line - 1; l > 0; l-- {
			t, _ := f.line(l)
			tr := strings.TrimRight(t, " \t")
			if tr == "" {
				continue
			}
			if strings.HasSuffix(tr, "}") {
				d.Fixes = append(d.Fixes, Fix{Title: "move `else` up to the closing brace", Applicability: MachineApplicable,
					Edits: []Edit{{File: f.path, Span: Span{File: f.path, Line: l, Col: len(tr) + 1, EndLine: d.Span.Line, EndCol: d.Span.Col}, NewText: " "}}})
			}
			break
		}
		return
	}
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
			d.Labels = append(d.Labels, Label{Span: sp, Text: "this expression is not finished"})
			d.Primary = "expected an operand here"
		}
		return
	}
}

// haveWant turns go/types "have X" and "want Y" detail lines into one
// expected/found note.
func haveWant(d *Diagnostic) {
	var have, want string
	var rest []string
	for _, n := range d.Notes {
		switch {
		case strings.HasPrefix(n, "have "):
			have = strings.TrimPrefix(n, "have ")
		case strings.HasPrefix(n, "want "):
			want = strings.TrimPrefix(n, "want ")
		default:
			rest = append(rest, n)
		}
	}
	if have == "" || want == "" {
		return
	}
	d.Notes = append([]string{fmt.Sprintf("expected `%s`, found `%s`", want, have)}, rest...)
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

var reDeclaredAt = lazy(`\(declared at ([^:]+):(\d+):(\d+)\)`)

var reButHave = lazy(`^undefined: (\w+)\.(\w+) \(but have (\w+)\)$`)

func (s *Sources) undefined(d *Diagnostic, f *srcFile, pos token.Pos) {
	if m := reButHave.FindStringSubmatch(d.Message); m != nil {
		if f.at(d.Span, m[2]) {
			setSpanLen(d, len(m[2]))
		}
		d.Primary = "not exported by package `" + m[1] + "`"
		d.Fixes = append(d.Fixes, Fix{Title: "use `" + m[1] + "." + m[3] + "`", Applicability: MachineApplicable,
			Edits: []Edit{{File: f.path, Span: d.Span, NewText: m[3]}}})
		return
	}
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
		d.Primary = "not declared by package `" + pkg + "`"
		cands := make([]string, 0, len(ex.names))
		for n := range ex.names {
			cands = append(cands, n)
		}
		if best := closest(sel, cands); best != "" {
			d.Fixes = append(d.Fixes, Fix{Title: "use `" + pkg + "." + best + "`", Applicability: MaybeIncorrect, Edits: []Edit{{File: f.path, Span: d.Span, NewText: best}}})
		} else if note := exportsNote(pkg, ex); note != "" {
			d.Notes = append(d.Notes, note)
		}
		return
	}
	if f.at(d.Span, name) {
		setSpanLen(d, len(name))
	}
	// A package name used without its import?
	if f.ast != nil && isPkgUse(f, pos) {
		if path, unique := s.stdPkgFor(name, selectorAfter(f, d.Span)); path != "" {
			d.Primary = "package `" + name + "` is not imported"
			if !unique {
				d.Help = append(d.Help, fmt.Sprintf("add `import %q`", path))
			}
			if unique {
				d.Fixes = append(d.Fixes, Fix{Title: fmt.Sprintf("add `import %q`", path), Applicability: MachineApplicable, Edits: []Edit{addImportEdit(f, path)}})
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
				d.Primary = "not declared in this scope"
				d.Help = append(d.Help, fmt.Sprintf("the standard library package `%s` (`import %q`) declares `%s`", best, path, sel))
				return
			}
		}
	}
	// Did you mean another name in scope?
	cands := s.scopeNames(f, pos)
	if best := closest(name, cands); best != "" {
		d.Primary = "not declared in this scope"
		d.Fixes = append(d.Fixes, Fix{Title: "use `" + best + "`", Applicability: MaybeIncorrect, Edits: []Edit{{File: f.path, Span: d.Span, NewText: best}}})
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
	limit := max(1, (len(name)+1)/4)
	sort.Strings(cands)
	for _, c := range cands {
		if c == name {
			return "" // the name exists; a suggestion would be noise
		}
		if strings.EqualFold(c, name) {
			return c
		}
		if len(name) >= 4 && affix(name, c) >= len(name) && len(c) <= 2*len(name) && bestD > 1 {
			// A word inserted in the middle: FormatName for FormatFullName.
			best, bestD = c, 2
			continue
		}
		if abs(len(c)-len(name)) > limit {
			continue
		}
		d := editDistance(strings.ToLower(name), strings.ToLower(c))
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

// backtickList formats names as `a`, `b`, `c`.
func backtickList(xs []string) string {
	var b strings.Builder
	for i, x := range xs {
		if i > 0 {
			b.WriteString(", ")
		}
		if strings.HasPrefix(x, "and ") {
			b.WriteString(x)
			continue
		}
		b.WriteString("`" + x + "`")
	}
	return b.String()
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
		d.Primary = "`" + typ + "` has no field or method `" + sel + "`"
		d.Fixes = append(d.Fixes, Fix{Title: "use `" + m[5] + "`", Applicability: MaybeIncorrect, Edits: []Edit{{File: f.path, Span: d.Span, NewText: m[5]}}})
		return
	}
	base := strings.TrimPrefix(typ, "*")
	ex := s.typeExports(f, base)
	if ex == nil {
		d.Primary = "`" + typ + "` has no field or method `" + sel + "`"
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
	d.Primary = "`" + typ + "` has no field or method `" + sel + "`"
	if best := closest(sel, cands); best != "" {
		d.Fixes = append(d.Fixes, Fix{Title: "use `" + best + "`", Applicability: MaybeIncorrect, Edits: []Edit{{File: f.path, Span: d.Span, NewText: best}}})
	}
	if fl := ex.fields[tname]; len(fl) > 0 {
		d.Notes = append(d.Notes, fmt.Sprintf("fields of `%s`: %s", base, backtickList(fl)))
	}
	if len(set) > 0 {
		if len(set) > 12 {
			set = append(set[:12], fmt.Sprintf("and %d more", len(set)-12))
		}
		d.Notes = append(d.Notes, fmt.Sprintf("methods of `%s`: %s", base, backtickList(set)))
		if strings.HasPrefix(typ, "*") == false {
			for _, x := range set {
				if strings.HasSuffix(x, ") "+sel) && strings.HasPrefix(x, "(*") {
					d.Help = append(d.Help, "`"+sel+"` has a pointer receiver: call it on a pointer or an addressable value")
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
	if strings.HasPrefix(d.Message, "label ") && strings.HasSuffix(d.Message, " declared and not used") {
		name := strings.Fields(d.Message)[1]
		setSpanLen(d, len(name)+1)
		d.Primary = "declared here but never used"
		if t, ok := f.line(d.Span.Line); ok && strings.TrimSpace(t) == name+":" {
			d.Fixes = append(d.Fixes, Fix{Title: "remove the label", Applicability: MachineApplicable,
				Edits: []Edit{{File: f.path, Span: Span{File: f.path, Line: d.Span.Line, Col: 1, EndLine: d.Span.Line + 1, EndCol: 1}}}})
		}
		return
	}
	m := reUnusedVar.FindStringSubmatch(d.Message)
	if m == nil || f.ast == nil {
		return
	}
	name := m[1]
	if f.at(d.Span, name) {
		setSpanLen(d, len(name))
	}
	d.Primary = "declared here but never used"
	if outer, ok := outerDecl(f, pos, name); ok {
		d.Code = CodeShadow
		d.Primary = "this `" + name + "` is a new variable that shadows the outer one"
		d.Labels = append(d.Labels, Label{Span: outer, Text: "the outer `" + name + "` is declared here and is not assigned"})
		d.Help = append(d.Help, "to assign the outer `"+name+"`, declare the other variables before the statement and use `=` instead of `:=`")
		return
	}
	path := pathNodes(f.ast, pos)
	for i := len(path) - 1; i >= 0; i-- {
		switch n := path[i].(type) {
		case *ast.AssignStmt:
			if n.Tok != token.DEFINE {
				return
			}
			if len(n.Lhs) == 1 && len(n.Rhs) == 1 && pure(n.Rhs[0]) {
				if e, ok := deleteLines(f, n); ok {
					d.Fixes = append(d.Fixes, Fix{Title: "remove `" + name + "`", Applicability: MachineApplicable, Edits: []Edit{e}})
					return
				}
			}
			if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
				sp := f.spanOf(n.Lhs[0])
				tp := f.fset.Position(n.TokPos)
				sp.EndLine, sp.EndCol = tp.Line, tp.Column+2
				d.Fixes = append(d.Fixes, Fix{Title: "assign the value to `_`", Applicability: MachineApplicable, Edits: []Edit{{File: f.path, Span: sp, NewText: "_ ="}}})
				return
			}
			others := 0
			for _, l := range n.Lhs {
				if id, ok := l.(*ast.Ident); ok && id.Name != "_" && id.Name != name {
					others++
				}
			}
			if others > 0 {
				d.Fixes = append(d.Fixes, Fix{Title: "replace `" + name + "` with `_`", Applicability: MaybeIncorrect, Edits: []Edit{{File: f.path, Span: d.Span, NewText: "_"}}})
			}
			return
		case *ast.RangeStmt:
			if id, ok := n.Value.(*ast.Ident); ok && id.Pos() == pos {
				// for k, v := range: drop ", v".
				ke := f.fset.Position(n.Key.End())
				sp := Span{File: f.path, Line: ke.Line, Col: ke.Column, EndLine: d.Span.EndLine, EndCol: d.Span.EndCol}
				d.Fixes = append(d.Fixes, Fix{Title: "remove `" + name + "`", Applicability: MachineApplicable, Edits: []Edit{{File: f.path, Span: sp, NewText: ""}}})
				return
			}
			if id, ok := n.Key.(*ast.Ident); ok && id.Pos() == pos {
				if n.Value == nil {
					// for i := range xs: drop "i := ".
					sp := f.spanOf(n.Key)
					tp := f.fset.Position(n.TokPos)
					sp.EndLine, sp.EndCol = tp.Line, tp.Column+3
					d.Fixes = append(d.Fixes, Fix{Title: "remove `" + name + "`", Applicability: MachineApplicable, Edits: []Edit{{File: f.path, Span: sp, NewText: ""}}})
				} else {
					d.Fixes = append(d.Fixes, Fix{Title: "replace `" + name + "` with `_`", Applicability: MaybeIncorrect, Edits: []Edit{{File: f.path, Span: d.Span, NewText: "_"}}})
				}
			}
			return
		case *ast.DeclStmt:
			gd := n.Decl.(*ast.GenDecl)
			if len(gd.Specs) == 1 && len(gd.Specs[0].(*ast.ValueSpec).Names) == 1 {
				if e, ok := deleteLines(f, n); ok {
					d.Fixes = append(d.Fixes, Fix{Title: "remove the declaration of `" + name + "`", Applicability: MachineApplicable, Edits: []Edit{e}})
				}
			}
			return
		}
	}
}

// localDecl finds the declaration of a local variable or parameter named
// name that is closest before pos in the enclosing function.
func (s *Sources) localDecl(f *srcFile, pos token.Pos, name string) (Span, bool) {
	fd := enclosingFunc(f, pos)
	if fd == nil || !isIdentName(name) {
		return Span{}, false
	}
	var best *ast.Ident
	consider := func(id *ast.Ident) {
		if id.Name == name && id.Pos() < pos && (best == nil || id.Pos() > best.Pos()) {
			best = id
		}
	}
	for _, fl := range fd.Type.Params.List {
		for _, n := range fl.Names {
			consider(n)
		}
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if n == nil || n.Pos() >= pos {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, l := range n.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						consider(id)
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range n.Names {
				consider(id)
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{n.Key, n.Value} {
				if id, ok := e.(*ast.Ident); ok {
					consider(id)
				}
			}
		}
		return true
	})
	if best == nil {
		return Span{}, false
	}
	return f.spanOf(best), true
}

func isIdentName(s string) bool {
	if s == "" || !isIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentByte(s[i]) {
			return false
		}
	}
	return true
}

// outerDecl finds a declaration of name in the enclosing function, before
// pos and outside the innermost block that contains pos: the variable that
// a := at pos shadows.
func outerDecl(f *srcFile, pos token.Pos, name string) (Span, bool) {
	fd := enclosingFunc(f, pos)
	if fd == nil {
		return Span{}, false
	}
	path := pathNodes(f.ast, pos)
	var inner ast.Node
	for i := len(path) - 1; i >= 0; i-- {
		if b, ok := path[i].(*ast.BlockStmt); ok {
			inner = b
			break
		}
	}
	var found Span
	ok := false
	consider := func(id *ast.Ident) {
		if id.Name == name && id.Pos() < pos && (inner == nil || id.Pos() < inner.Pos() || id.Pos() >= inner.End()) {
			found, ok = f.spanOf(id), true
		}
	}
	for _, fl := range fd.Type.Params.List {
		for _, n := range fl.Names {
			consider(n)
		}
	}
	if fd.Type.Results != nil {
		for _, fl := range fd.Type.Results.List {
			for _, n := range fl.Names {
				consider(n)
			}
		}
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if n == nil || n.Pos() >= pos {
			return false
		}
		// Only declarations in blocks that enclose pos are visible there.
		if b, isBlock := n.(*ast.BlockStmt); isBlock && !(b.Pos() <= pos && pos < b.End()) {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, l := range n.Lhs {
					if id, isID := l.(*ast.Ident); isID {
						consider(id)
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range n.Names {
				consider(id)
			}
		}
		return true
	})
	return found, ok
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
	d.Primary = "imported here but not used"
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
				d.Fixes = append(d.Fixes, Fix{Title: fmt.Sprintf("remove the import of `%q`", path), Applicability: MachineApplicable, Edits: []Edit{e}})
			}
			d.Help = append(d.Help, fmt.Sprintf("if it is imported only for its side effects, write `import _ %q`", path))
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
	d.Primary = fmt.Sprintf("expected `%s`, found `%s`", want, haveT)
	if sp, ok := s.localDecl(f, pos, expr); ok && !strings.HasPrefix(haveT, "untyped") {
		labelOrRelated(d, sp, "`"+expr+"` is declared here as `"+haveT+"`")
	}
	if strings.HasSuffix(d.Message, "(overflows)") {
		d.Primary = fmt.Sprintf("`%s` does not fit in `%s`", expr, want)
		if r, ok := intRange[want]; ok {
			d.Notes = append(d.Notes, fmt.Sprintf("`%s` holds %s", want, r))
		}
		d.Help = append(d.Help, "use a wider type, such as `int`")
		return
	}
	if callee != "" {
		arg := -1
		if f.ast != nil {
			for _, n := range pathNodes(f.ast, pos) {
				if ce, ok := n.(*ast.CallExpr); ok {
					for i, a := range ce.Args {
						if a.Pos() == pos {
							arg = i
						}
					}
				}
			}
		}
		s.relatedFuncArg(d, f, callee, arg)
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
		return fmt.Sprintf("convert the value: `%s(%s)`", want, expr)
	case isStr(have) && isInt(want):
		return fmt.Sprintf("to parse a number from text, use `strconv.Atoi(%s)`", expr)
	case isInt(have) && isStr(want):
		return fmt.Sprintf("to format a number as text, use `strconv.Itoa(%s)` (`string(%s)` gives the character with that code point)", expr, expr)
	case have == "[]byte" && isStr(want):
		return fmt.Sprintf("convert the bytes: `string(%s)`", expr)
	case isStr(have) && want == "[]byte":
		return fmt.Sprintf("convert the string: `[]byte(%s)`", expr)
	}
	return ""
}

// relatedFunc adds the declaration of the named function: a label when it
// is in the same file, a related location otherwise, or a note with the
// signature for an imported function. arg is the index of the argument the
// diagnostic is about (-1 for none); its parameter gets the label.
func (s *Sources) relatedFunc(d *Diagnostic, f *srcFile, name string) {
	s.relatedFuncArg(d, f, name, -1)
}

func (s *Sources) relatedFuncArg(d *Diagnostic, f *srcFile, name string, arg int) {
	name = strings.TrimPrefix(name, "(")
	// Method expressions and pkg.F: use the last element.
	if i := strings.LastIndex(name, "."); i >= 0 {
		pkg := name[:i]
		fn := name[i+1:]
		if ex := s.importedExports(f, pkg); ex != nil {
			_ = ex
			if sig := s.importedFuncSig(f, pkg, fn); sig != "" {
				d.Notes = append(d.Notes, fmt.Sprintf("`%s.%s` is declared as `%s`", pkg, fn, sig))
			}
			return
		}
		name = fn
	}
	for _, pf := range s.pkgOf(f.path).files {
		for _, dl := range pf.ast.Decls {
			if fd, ok := dl.(*ast.FuncDecl); ok && fd.Name.Name == name {
				sp, text := pf.spanOf(fd.Type), "`"+name+"` is declared here"
				if field, pname := paramAt(fd, arg); field != nil {
					sp = pf.spanOf(field.Type)
					text = "expected because of this parameter"
					if pname != "" {
						text = "expected because of parameter `" + pname + "`"
					}
				}
				if pf == f {
					d.Labels = append(d.Labels, Label{Span: sp, Text: text})
				} else {
					d.Related = append(d.Related, Related{Span: sp, Message: text})
				}
				return
			}
		}
	}
}

// paramAt returns the field and name of parameter i of fd (a variadic last
// parameter covers the rest).
func paramAt(fd *ast.FuncDecl, i int) (*ast.Field, string) {
	if i < 0 || fd.Type.Params == nil {
		return nil, ""
	}
	k := 0
	list := fd.Type.Params.List
	for fi, fl := range list {
		n := max(len(fl.Names), 1)
		if i < k+n || fi == len(list)-1 {
			if _, ok := fl.Type.(*ast.Ellipsis); ok || i < k+n {
				if len(fl.Names) > 0 {
					return fl, fl.Names[min(i-k, len(fl.Names)-1)].Name
				}
				return fl, ""
			}
		}
		k += n
	}
	return nil, ""
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
		d.Labels = append(d.Labels, Label{Span: f.spanOf(be.X), Text: "`" + lt + "`"}, Label{Span: f.spanOf(be.Y), Text: "`" + rt + "`"})
		for _, side := range []struct {
			e ast.Expr
			t string
		}{{be.X, lt}, {be.Y, rt}} {
			if id, ok := side.e.(*ast.Ident); ok {
				if sp, ok := s.localDecl(f, pos, id.Name); ok && sp.Line != d.Span.Line {
					labelOrRelated(d, sp, "`"+id.Name+"` is `"+side.t+"` from here")
				}
			}
		}
		if numeric[lt] && numeric[rt] {
			// Convert toward floating point so no fraction is lost.
			if strings.HasPrefix(rt, "float") && !strings.HasPrefix(lt, "float") {
				d.Help = append(d.Help, fmt.Sprintf("convert one operand, for example `%s(%s) %s %s`", rt, f.text(be.X), be.Op, f.text(be.Y)))
			} else {
				d.Help = append(d.Help, fmt.Sprintf("convert one operand, for example `%s %s %s(%s)`", f.text(be.X), be.Op, lt, f.text(be.Y)))
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
		d.Labels = append(d.Labels, Label{Span: f.spanOf(lit.Type), Text: "the results are declared here"})
		return
	}
	if fd.Type.Results == nil || len(fd.Type.Results.List) == 0 {
		d.Primary = "returns a value"
		d.Labels = append(d.Labels, Label{Span: f.spanOf(fd.Type), Text: "`" + fd.Name.Name + "` declares no results"})
		if fd.Name.Name != "main" && fd.Name.Name != "init" {
			d.Help = append(d.Help, fmt.Sprintf("to return a value, declare a result type: `%s T`", oneLine(f.text(fd.Type))))
		}
	} else {
		d.Primary = "returns a different number of values"
		d.Labels = append(d.Labels, Label{Span: f.spanOf(fd.Type.Results), Text: "the signature declares these results"})
	}
}

func (s *Sources) argCount(d *Diagnostic, f *srcFile, pos token.Pos) {
	m := reArgCount.FindStringSubmatch(d.Message)
	if m == nil {
		return
	}
	if strings.HasPrefix(d.Message, "too many") {
		d.Primary = "unexpected argument"
	} else {
		d.Primary = "missing arguments"
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
		d.Primary = fmt.Sprintf("`%s` returns %d values", m[2], nr)
		d.Help = append(d.Help, fmt.Sprintf("receive every result: `%s %s %s`", strings.Join(names, ", "), as.Tok, f.text(as.Rhs[0])))
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
			d.Primary = "no new variables on the left"
			d.Fixes = append(d.Fixes, Fix{Title: "use `=` instead of `:=`", Applicability: MachineApplicable, Edits: []Edit{{File: f.path, Span: sp, NewText: "="}}})
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

var (
	reNotImpl = lazy(`(?:^cannot use (.+?) \((.+?)\) as (\S+) value in (.+?): )?(\*?[\w.]+) does not implement ([\w.]+) \((.+)\)$`)
	reNotSat  = lazy(`^(\S+) does not satisfy ([\w.]+)(?: \((?:(\S+) missing in (.+)|(.+))\))?$`)
)

// ifaceMethod is one method an interface requires.
type ifaceMethod struct {
	name, sig string // sig is the method type without "func", e.g. "() float64"
	span      Span
}

// typeMethod is one method declared on a named type.
type typeMethod struct {
	name, sig string
	ptr       bool // pointer receiver
	span      Span // the method name
	recv      Span // the receiver type
}

// interfaceMethods finds the declaration of interface name in the package
// of f (embedded local interfaces included).
func (s *Sources) interfaceMethods(f *srcFile, name string, depth int) ([]ifaceMethod, Span, bool) {
	if depth > 4 {
		return nil, Span{}, false
	}
	for _, pf := range s.pkgOf(f.path).files {
		for _, dl := range pf.ast.Decls {
			gd, ok := dl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				ts := sp.(*ast.TypeSpec)
				it, ok := ts.Type.(*ast.InterfaceType)
				if ts.Name.Name != name || !ok {
					continue
				}
				var ms []ifaceMethod
				for _, m := range it.Methods.List {
					if ft, ok := m.Type.(*ast.FuncType); ok && len(m.Names) > 0 {
						ms = append(ms, ifaceMethod{name: m.Names[0].Name, sig: sigTypes(pf, ft), span: pf.spanOf(m)})
					} else if id, ok := m.Type.(*ast.Ident); ok {
						sub, _, _ := s.interfaceMethods(f, id.Name, depth+1)
						ms = append(ms, sub...)
					}
				}
				return ms, pf.spanOf(ts.Name), true
			}
		}
	}
	return nil, Span{}, false
}

// typeMethods lists the methods declared on the named type in the package
// of f.
func (s *Sources) typeMethods(f *srcFile, name string) []typeMethod {
	var out []typeMethod
	for _, pf := range s.pkgOf(f.path).files {
		for _, dl := range pf.ast.Decls {
			fd, ok := dl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
				continue
			}
			t := fd.Recv.List[0].Type
			ptr := false
			if st, ok := t.(*ast.StarExpr); ok {
				t, ptr = st.X, true
			}
			if id, ok := t.(*ast.Ident); ok && id.Name == name {
				out = append(out, typeMethod{name: fd.Name.Name, sig: sigTypes(pf, fd.Type),
					ptr: ptr, span: pf.spanOf(fd.Name), recv: pf.spanOf(fd.Recv.List[0].Type)})
			}
		}
	}
	return out
}

// labelOrRelated adds a secondary span: a label when it is in the
// diagnostic's file, a related location otherwise.
func labelOrRelated(d *Diagnostic, sp Span, text string) {
	if sp.File == d.Span.File {
		d.Labels = append(d.Labels, Label{Span: sp, Text: text})
	} else {
		d.Related = append(d.Related, Related{Span: sp, Message: text})
	}
}

// notImplement explains an interface satisfaction failure from what the
// user wrote: every method the interface needs that the type lacks, with
// the reason for each, the closest names, and the declarations involved.
func (s *Sources) notImplement(d *Diagnostic, f *srcFile, pos token.Pos) {
	m := reNotImpl.FindStringSubmatch(d.Message)
	if m == nil {
		return
	}
	expr, typ, iface := m[1], m[5], m[6]
	if expr != "" && f.ast != nil {
		for _, n := range pathNodes(f.ast, pos) {
			if e, ok := n.(ast.Expr); ok && e.Pos() == pos {
				d.Span = f.spanOf(e)
				break
			}
		}
	}
	isPtr := strings.HasPrefix(typ, "*")
	tname := strings.TrimPrefix(typ, "*")
	d.Primary = "`" + typ + "` does not implement `" + iface + "`"
	if strings.Contains(tname, ".") {
		return // a type from another package: go/types' reason is all we have
	}
	need, ispan, ok := s.interfaceMethods(f, iface, 0)
	if iface == "error" {
		need, ok = []ifaceMethod{{name: "Error", sig: "() string"}}, true
	} else if pkg, in, isQual := strings.Cut(iface, "."); isQual {
		need, ispan, ok = s.importedInterface(f, pkg, in)
	}
	if !ok {
		return
	}
	have := s.typeMethods(f, tname)
	byName := map[string]typeMethod{}
	var names []string
	for _, h := range have {
		byName[h.name] = h
		names = append(names, h.name)
	}
	var lacks []string
	pointerN := 0
	for _, n := range need {
		h, found := byName[n.name]
		switch {
		case !found:
			why := "`" + n.name + n.sig + "` is missing"
			if best := closest(n.name, names); best != "" {
				why += "; `" + tname + "` has `" + best + "`"
				labelOrRelated(d, byName[best].span, "`"+best+"` is declared here; `"+iface+"` needs `"+n.name+"`")
			}
			lacks = append(lacks, why)
		case normSig(h.sig) != normSig(n.sig):
			lacks = append(lacks, "`"+n.name+"` has type `func"+h.sig+"`, `"+iface+"` needs `func"+n.sig+"`")
			labelOrRelated(d, h.span, "`"+n.name+"` has the wrong type")
		case h.ptr && !isPtr:
			lacks = append(lacks, "`"+n.name+"` has a pointer receiver, so only `*"+tname+"` has it")
			if pointerN == 0 {
				labelOrRelated(d, h.recv, "`"+n.name+"` has a pointer receiver")
			}
			pointerN++
		}
	}
	if len(lacks) == 0 {
		return
	}
	if ispan.Valid() {
		labelOrRelated(d, ispan, "`"+iface+"` is declared here")
	}
	d.Notes = nil // the have/want lines of go/types are in the list below
	plural := "method"
	if len(lacks) > 1 {
		plural = "methods"
	}
	d.Notes = append(d.Notes, fmt.Sprintf("`%s` lacks %d %s of `%s`:", typ, len(lacks), plural, iface))
	for _, l := range lacks {
		d.Notes = append(d.Notes, "  "+l)
	}
	if pointerN > 0 && pointerN == len(lacks) && expr != "" && d.Span.Valid() {
		d.Fixes = append(d.Fixes, Fix{Title: "pass a pointer", Applicability: MaybeIncorrect,
			Edits: []Edit{{File: f.path, Span: Span{File: f.path, Line: d.Span.Line, Col: d.Span.Col, EndLine: d.Span.Line, EndCol: d.Span.Col}, NewText: "&"}}})
	}
}

// importedInterface reads the methods of interface pkg.name from the
// imported package's source.
func (s *Sources) importedInterface(f *srcFile, pkg, name string) ([]ifaceMethod, Span, bool) {
	path, ok := s.importFor(f, pkg)
	if !ok {
		return nil, Span{}, false
	}
	dir := s.pkgDir(f.path, path)
	if dir == "" {
		return nil, Span{}, false
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
			gd, isGen := dl.(*ast.GenDecl)
			if !isGen || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				ts := sp.(*ast.TypeSpec)
				it, isIface := ts.Type.(*ast.InterfaceType)
				if ts.Name.Name != name || !isIface {
					continue
				}
				var ms []ifaceMethod
				for _, m := range it.Methods.List {
					if ft, isFunc := m.Type.(*ast.FuncType); isFunc && len(m.Names) > 0 {
						ms = append(ms, ifaceMethod{name: m.Names[0].Name, sig: sigTypes(pf, ft)})
					}
				}
				return ms, Span{}, true
			}
		}
	}
	return nil, Span{}, false
}

// normSig removes white space so signatures compare by text.
func normSig(sig string) string {
	return strings.ReplaceAll(sig, " ", "")
}

// sigTypes renders the parameter and result types of a function type
// without names: "(int, string) error".
func sigTypes(f *srcFile, ft *ast.FuncType) string {
	list := func(fl *ast.FieldList) []string {
		var out []string
		if fl == nil {
			return nil
		}
		for _, x := range fl.List {
			t := oneLine(f.text(x.Type))
			for range max(len(x.Names), 1) {
				out = append(out, t)
			}
		}
		return out
	}
	b := "(" + strings.Join(list(ft.Params), ", ") + ")"
	res := list(ft.Results)
	switch len(res) {
	case 0:
	case 1:
		b += " " + res[0]
	default:
		b += " (" + strings.Join(res, ", ") + ")"
	}
	return b
}

// notSatisfy explains a type argument that is not in a constraint's type
// set: what was inferred from which argument, the type set, and the
// underlying type when that is what the set misses.
func (s *Sources) notSatisfy(d *Diagnostic, f *srcFile, pos token.Pos) {
	m := reNotSat.FindStringSubmatch(d.Message)
	if m == nil {
		return
	}
	typ, cons, set := m[1], m[2], m[4]
	d.Primary = "`" + typ + "` does not satisfy `" + cons + "`"
	if cons == "comparable" {
		d.Notes = append(d.Notes, "`comparable` holds the types that support `==`; function, slice and map types do not, nor structs and arrays that contain them")
	}
	if f.ast != nil {
		for _, n := range pathNodes(f.ast, pos) {
			ce, ok := n.(*ast.CallExpr)
			if !ok || ce.Fun.Pos() != pos {
				continue
			}
			d.Span = f.spanOf(ce.Fun)
			if len(ce.Args) > 0 {
				labelOrRelated(d, f.spanOf(ce.Args[0]), "the type argument is inferred as `"+typ+"` from this")
			}
		}
	}
	if set != "" {
		d.Notes = append(d.Notes, fmt.Sprintf("the type set of `%s` is `%s`", cons, set))
	}
	// A local defined type whose underlying type is close: say so.
	if ex := s.localExports(f); !strings.Contains(typ, ".") {
		if def := ex.decl[typ]; def != "" {
			under := strings.TrimSpace(strings.TrimPrefix(def, "type "+typ))
			if under != "" && !strings.ContainsAny(under, "{(") {
				d.Notes = append(d.Notes, fmt.Sprintf("`%s` has underlying type `%s`, which the type set does not include", typ, under))
				if numeric[under] {
					d.Help = append(d.Help, fmt.Sprintf("to accept it, add `~%s` to `%s`", under, cons))
				}
			}
		}
	}
	if numeric[typ] && set != "" {
		for _, t := range []string{"float64", "int", "int64", "float32"} {
			if strings.Contains(set, "~"+t) || strings.Contains(set, " "+t) {
				d.Help = append(d.Help, fmt.Sprintf("use a type in the set, for example `%s` instead of `%s`", t, typ))
				break
			}
		}
	}
	if _, sp, ok := s.constraintDecl(f, cons); ok {
		labelOrRelated(d, sp, "`"+cons+"` is declared here")
	}
}

// constraintDecl finds a local interface type declaration.
func (s *Sources) constraintDecl(f *srcFile, name string) (*ast.TypeSpec, Span, bool) {
	for _, pf := range s.pkgOf(f.path).files {
		for _, dl := range pf.ast.Decls {
			gd, ok := dl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, sp := range gd.Specs {
				if ts := sp.(*ast.TypeSpec); ts.Name.Name == name {
					return ts, pf.spanOf(ts.Name), true
				}
			}
		}
	}
	return nil, Span{}, false
}

var intRange = map[string]string{
	"byte": "0 to 255", "uint8": "0 to 255", "int8": "-128 to 127",
	"uint16": "0 to 65535", "int16": "-32768 to 32767",
	"uint32": "0 to 4294967295", "int32": "-2147483648 to 2147483647", "rune": "-2147483648 to 2147483647",
}

// invalidOp explains assignments to constants and strings, calls of
// values, and non-boolean conditions.
func (s *Sources) invalidOp(d *Diagnostic, f *srcFile, pos token.Pos) {
	msg := d.Message
	switch {
	case strings.HasPrefix(msg, "cannot assign to ") && f.ast != nil:
		for _, n := range pathNodes(f.ast, pos) {
			ix, ok := n.(*ast.IndexExpr)
			if !ok || ix.Pos() != pos {
				continue
			}
			d.Span = f.spanOf(ix)
			if id, isID := ix.X.(*ast.Ident); isID && s.isStringVar(f, pos, id.Name) {
				d.Primary = "strings are immutable"
				d.Help = append(d.Help, fmt.Sprintf("convert to bytes, change them, and convert back: `b := []byte(%s); b[i] = ...; %s = string(b)`", id.Name, id.Name))
			}
			return
		}
		name := strings.Fields(strings.TrimPrefix(msg, "cannot assign to "))[0]
		if ex := s.localExports(f); ex.names[name] == "const" {
			d.Primary = "`" + name + "` is a constant"
			if sp, ok := s.declSpan(f, name); ok {
				labelOrRelated(d, sp, "declared as a constant here")
			}
			d.Help = append(d.Help, "to change the value at run time, declare it with `var`")
		}
	case strings.HasPrefix(msg, "invalid operation: cannot call "):
		name := strings.Fields(strings.TrimPrefix(msg, "invalid operation: cannot call "))[0]
		d.Primary = "`" + name + "` is not a function"
	case strings.HasPrefix(msg, "non-boolean condition"):
		d.Primary = "expected `bool`"
		d.Help = append(d.Help, "Go has no truthiness: compare explicitly, for example `x != 0`, `s != \"\"` or `p != nil`")
	}
}

// otherHints handles errors without a more specific code.
func (s *Sources) otherHints(d *Diagnostic, f *srcFile, pos token.Pos) {
	msg := d.Message
	switch {
	case strings.HasPrefix(msg, "invalid map key type") && f.ast != nil:
		for _, n := range pathNodes(f.ast, pos) {
			if e, ok := n.(ast.Expr); ok && e.Pos() == pos {
				d.Span = f.spanOf(e)
			}
		}
		d.Primary = "not comparable, so it cannot be a map key"
		d.Help = append(d.Help, "map keys must support `==`: use a string, a struct of comparable fields, or an array such as `[4]int`")
	case strings.HasSuffix(msg, "is not used") && f.ast != nil:
		d.Primary = "this value is not used"
		for _, n := range pathNodes(f.ast, pos) {
			if be, ok := n.(*ast.BinaryExpr); ok && be.Pos() == pos {
				d.Span = f.spanOf(be)
				if be.Op == token.EQL {
					op := f.fset.Position(be.OpPos)
					d.Fixes = append(d.Fixes, Fix{Title: "to assign, use `=`", Applicability: MaybeIncorrect,
						Edits: []Edit{{File: f.path, Span: Span{File: f.path, Line: op.Line, Col: op.Column, EndLine: op.Line, EndCol: op.Column + 2}, NewText: "="}}})
				}
				break
			}
		}
	case strings.HasPrefix(msg, "break not in") || strings.HasPrefix(msg, "continue not in"):
		d.Primary = "not inside a loop"
		d.Help = append(d.Help, "to leave a function early, use `return`")
	}
}

// isStringVar reports whether name is declared in the function around pos
// as a string (from a string literal or a string type).
func (s *Sources) isStringVar(f *srcFile, pos token.Pos, name string) bool {
	fd := enclosingFunc(f, pos)
	if fd == nil {
		return false
	}
	found := false
	ast.Inspect(fd, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, l := range n.Lhs {
				if id, ok := l.(*ast.Ident); ok && id.Name == name && i < len(n.Rhs) {
					if bl, ok := n.Rhs[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
						found = true
					}
				}
			}
		case *ast.ValueSpec:
			if id, ok := n.Type.(*ast.Ident); ok && id.Name == "string" {
				for _, nm := range n.Names {
					if nm.Name == name {
						found = true
					}
				}
			}
		}
		return true
	})
	return found
}

// declSpan finds the declaring identifier of a package level name.
func (s *Sources) declSpan(f *srcFile, name string) (Span, bool) {
	for _, pf := range s.pkgOf(f.path).files {
		for _, dl := range pf.ast.Decls {
			gd, ok := dl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, sp := range gd.Specs {
				if vs, ok := sp.(*ast.ValueSpec); ok {
					for _, n := range vs.Names {
						if n.Name == name {
							return pf.spanOf(n), true
						}
					}
				}
			}
		}
	}
	return Span{}, false
}

// exportsNote lists what a small package exports, functions first, or ""
// for a large package.
func exportsNote(pkg string, ex *exports) string {
	if len(ex.names) == 0 || len(ex.names) > 16 {
		return ""
	}
	var funcs, other []string
	for n, k := range ex.names {
		if k == "func" {
			funcs = append(funcs, n)
		} else {
			other = append(other, n)
		}
	}
	sort.Strings(funcs)
	sort.Strings(other)
	return fmt.Sprintf("package `%s` exports %s", pkg, backtickList(append(funcs, other...)))
}

var reUnknownField = lazy(`^unknown field (\w+) in struct literal of type (\S+)$`)

// unknownField suggests the closest field of the struct, from its
// declaration in this package or an imported one.
func (s *Sources) unknownField(d *Diagnostic, f *srcFile, pos token.Pos) {
	m := reUnknownField.FindStringSubmatch(d.Message)
	if m == nil {
		return
	}
	field, typ := m[1], strings.TrimPrefix(m[2], "*")
	d.Primary = "`" + typ + "` has no field `" + field + "`"
	ex := s.typeExports(f, typ)
	if ex == nil {
		return
	}
	tname := typ
	if _, after, ok := strings.Cut(typ, "."); ok {
		tname = after
	}
	fields := ex.fields[tname]
	if best := closest(field, fields); best != "" {
		d.Fixes = append(d.Fixes, Fix{Title: "use `" + best + "`", Applicability: MaybeIncorrect,
			Edits: []Edit{{File: f.path, Span: d.Span, NewText: best}}})
	} else if len(fields) > 0 {
		d.Notes = append(d.Notes, fmt.Sprintf("fields of `%s`: %s", typ, backtickList(fields)))
	}
}
