package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/Gaurav-Gosain/marmot/internal/driver"
	"github.com/Gaurav-Gosain/marmot/internal/repl"
)

// The REPL keeps a session (imports, declarations, statements) and runs
// each entry as a whole program built by the incremental build: the
// session's statements, then a marker, then the new entry. Only the output
// after the marker is shown, so earlier output is not repeated (earlier
// statements do run again; their side effects repeat). An entry that does
// not compile, or that panics, is not kept.

const replMark = "\x00marmot-repl\x00\n"

type replSession struct {
	imports map[string]string // path -> name used in the code
	decls   []string          // package level declarations
	stmts   []string          // body statements
	names   []string          // variables the statements declare
	dir     string
	root    string
	timing  bool
	src     *diag.Sources
	colors  bool
}

func newReplFlags() (*flag.FlagSet, *bool) {
	fs := flag.NewFlagSet("repl", flag.ContinueOnError)
	return fs, fs.Bool("time", false, "print how long each entry took to build and run")
}

func cmdRepl(args []string) int {
	fs, timing := newReplFlags()
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError("repl", err)
	}
	root, err := driver.FindRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot:", err)
		return 2
	}
	dir, err := os.MkdirTemp("", "marmot-repl-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot:", err)
		return 1
	}
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module repl\n\ngo 1.27\n"), 0o644)
	s := &replSession{imports: map[string]string{}, dir: dir, root: root, timing: *timing, src: diag.NewSources(),
		colors: diag.IsTerminal(os.Stdout) && os.Getenv("NO_COLOR") == ""}
	home, _ := os.UserHomeDir()
	p := repl.New(repl.Options{
		Prompt:      s.color("\x1b[1;36m", "go> "),
		Continue:    s.color("\x1b[2m", "... "),
		HistoryPath: filepath.Join(home, ".marmot_history"),
		Suggest:     s.suggest,
		Highlight:   s.highlight,
		Complete:    complete,
	})
	defer p.Close()
	if p.Interactive() {
		fmt.Println(s.color("\x1b[2m", "marmot repl: Go declarations, statements and expressions. :help for commands, ctrl-d to quit."))
	}
	for {
		line, err := p.ReadLine()
		if err == repl.ErrEOF {
			return 0
		}
		if err == repl.ErrCanceled {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ":") {
			if s.meta(line) {
				return 0
			}
			continue
		}
		s.eval(line)
	}
}

func (s *replSession) color(c, text string) string {
	if !s.colors {
		return text
	}
	return c + text + "\x1b[0m"
}

// complete reports whether brackets are balanced and the entry does not
// end in an operator or comma (an open entry continues on the next line).
func complete(in string) bool {
	depth := 0
	var sc scanner.Scanner
	fset := token.NewFileSet()
	f := fset.AddFile("", -1, len(in))
	sc.Init(f, []byte(in), func(token.Position, string) {}, 0)
	last := token.ILLEGAL
	for {
		_, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		switch tok {
		case token.LPAREN, token.LBRACE, token.LBRACK:
			depth++
		case token.RPAREN, token.RBRACE, token.RBRACK:
			depth--
		}
		if tok != token.SEMICOLON || lit != "\n" {
			last = tok
		}
	}
	if depth > 0 {
		return false
	}
	switch last {
	case token.COMMA, token.ADD, token.SUB, token.MUL, token.QUO, token.LAND, token.LOR, token.ASSIGN, token.DEFINE:
		return false
	}
	return true
}

func (s *replSession) meta(line string) (quit bool) {
	cmd, arg, _ := strings.Cut(line, " ")
	arg = strings.TrimSpace(arg)
	switch cmd {
	case ":q", ":quit", ":exit":
		return true
	case ":help", ":h":
		fmt.Print(`:type EXPR   print the type of an expression
:doc NAME    show the documentation of a package or name (go doc)
:time        print build and run time of each entry (toggle)
:imports     list the imports of the session
:reset       forget the session
:quit        leave (or ctrl-d)
`)
	case ":time":
		s.timing = !s.timing
		fmt.Println("timing", map[bool]string{true: "on", false: "off"}[s.timing])
	case ":imports":
		var ps []string
		for p := range s.imports {
			ps = append(ps, p)
		}
		sort.Strings(ps)
		for _, p := range ps {
			fmt.Println(p)
		}
	case ":reset":
		s.imports, s.decls, s.stmts, s.names = map[string]string{}, nil, nil, nil
	case ":type", ":t":
		s.autoImport(arg)
		s.run(fmt.Sprintf("__fmt.Printf(\"%%T\\n\", %s)", arg), "", false)
	case ":doc", ":d":
		c := exec.Command("go", "doc", arg)
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		c.Run()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %s (:help lists them)\n", cmd)
	}
	return false
}

var printCall = regexp.MustCompile(`^(fmt\.(Print|Fprint|Sprint)|println|print|log\.|os\.Exit|panic)`)

// eval runs one entry: an import, declarations, an expression (printed),
// or statements.
func (s *replSession) eval(in string) {
	if strings.HasPrefix(in, "import ") {
		s.addImports(in)
		return
	}
	if isDecl(in) {
		s.autoImport(in)
		if s.run("", in, true) {
			s.decls = append(s.decls, in)
		}
		return
	}
	s.autoImport(in)
	if e, err := parser.ParseExpr(in); err == nil {
		_, isCall := e.(*ast.CallExpr)
		if !isCall || !printCall.MatchString(in) {
			if s.run("__fmt.Println("+in+")", "", isCall) {
				return
			}
			if isCall {
				// A call with no results, or a wrong call: run it as a
				// statement, which reports the errors of the call itself.
				s.runStmt(in, false)
			}
			return
		}
	}
	s.runStmt(in, false)
}

func (s *replSession) runStmt(in string, quiet bool) {
	if s.run(in, "", quiet) {
		s.stmts = append(s.stmts, in)
		s.names = append(s.names, definedNames(in)...)
	}
}

func isDecl(in string) bool {
	for _, kw := range []string{"func ", "type ", "const "} {
		if strings.HasPrefix(in, kw) {
			return true
		}
	}
	return false
}

// definedNames returns the variables an entry declares at its top level
// (x := ..., var x); names declared inside blocks or statement headers are
// not visible afterwards.
func definedNames(in string) []string {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\nfunc _() {\n"+in+"\n}", 0)
	if err != nil {
		return nil
	}
	var out []string
	add := func(id *ast.Ident) {
		if id.Name != "_" {
			out = append(out, id.Name)
		}
	}
	for _, st := range f.Decls[0].(*ast.FuncDecl).Body.List {
		switch st := st.(type) {
		case *ast.AssignStmt:
			if st.Tok == token.DEFINE {
				for _, l := range st.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						add(id)
					}
				}
			}
		case *ast.DeclStmt:
			if gd, ok := st.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
				for _, sp := range gd.Specs {
					for _, id := range sp.(*ast.ValueSpec).Names {
						add(id)
					}
				}
			}
		}
	}
	return out
}

func (s *replSession) addImports(in string) {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+in, parser.ImportsOnly)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return
	}
	for _, is := range f.Imports {
		p := strings.Trim(is.Path.Value, `"`)
		name := filepath.Base(p)
		if is.Name != nil {
			name = is.Name.Name
		}
		s.imports[p] = name
	}
}

var qualRE = regexp.MustCompile(`\b([a-z][a-z0-9]*)\.([A-Z]\w*)`)

// autoImport imports the standard packages an entry uses by name.
func (s *replSession) autoImport(in string) {
	used := map[string]bool{}
	for _, n := range s.imports {
		used[n] = true
	}
	for _, m := range qualRE.FindAllStringSubmatch(in, -1) {
		if used[m[1]] || s.isVar(m[1]) {
			continue
		}
		if p, ok := s.src.StdImport(m[1], m[2]); ok {
			s.imports[p] = m[1]
			used[m[1]] = true
		}
	}
}

func (s *replSession) isVar(name string) bool {
	return slices.Contains(s.names, name)
}

// program writes the session plus entry as main.go.
func (s *replSession) program(entry, decl string) string {
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n\t__fmt \"fmt\"\n\t__os \"os\"\n")
	code := strings.Join(s.decls, "\n") + decl + strings.Join(s.stmts, "\n") + entry
	var paths []string
	for p := range s.imports {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		name := s.imports[p]
		if !strings.Contains(code, name+".") {
			continue // unused imports would not compile
		}
		fmt.Fprintf(&b, "\t%s %q\n", name, p)
	}
	b.WriteString(")\n\nvar _ = __fmt.Sprint\n\n")
	for _, d := range s.decls {
		b.WriteString(d + "\n\n")
	}
	if decl != "" {
		b.WriteString(decl + "\n\n")
	}
	b.WriteString("func main() {\n")
	for _, st := range s.stmts {
		b.WriteString(st + "\n")
	}
	for _, n := range s.names {
		b.WriteString("_ = " + n + "\n")
	}
	b.WriteString("__os.Stdout.WriteString(" + fmt.Sprintf("%q", replMark) + ")\n")
	b.WriteString("__os.Stderr.WriteString(" + fmt.Sprintf("%q", replMark) + ")\n")
	if entry != "" {
		b.WriteString(entry + "\n")
		for _, n := range definedNames(entry) {
			b.WriteString("_ = " + n + "\n")
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// run builds and runs the session with the entry and reports whether both
// worked. Build errors are printed unless quiet.
func (s *replSession) run(entry, decl string, quiet bool) bool {
	src := s.program(entry, decl)
	os.WriteFile(filepath.Join(s.dir, "main.go"), []byte(src), 0o644)
	start := time.Now()
	exe := filepath.Join(s.dir, "prog")
	cfg := driver.Config{Root: s.root, Dir: s.dir, Output: exe, OptLevel: "2", Emit: "exe", GC: "precise",
		Dialect: "llvm", Patterns: []string{"."}, Stderr: io.Discard, AllErrors: true}
	if _, err := driver.Build(cfg); err != nil {
		if !quiet {
			s.showErrors(err, src)
		}
		return false
	}
	built := time.Since(start)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	var out, errb bytes.Buffer
	cmd.Stdin = os.Stdin
	cmd.Stdout, cmd.Stderr = &markWriter{w: os.Stdout, buf: &out}, &markWriter{w: os.Stderr, buf: &errb}
	cmd.Env = append(os.Environ(), "MARMOT_OUTPUT=plain")
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)
	go func() {
		for range sigs {
			cancel()
		}
	}()
	err := cmd.Run()
	signal.Stop(sigs)
	close(sigs)
	if s.timing {
		fmt.Fprintln(os.Stderr, s.color("\x1b[2m", fmt.Sprintf("(build %v, run %v)", built.Round(time.Millisecond), (time.Since(start)-built).Round(time.Millisecond))))
	}
	return err == nil
}

// markWriter passes output through only after the session marker.
type markWriter struct {
	w    io.Writer
	buf  *bytes.Buffer
	open bool
}

func (m *markWriter) Write(p []byte) (int, error) {
	if m.open {
		return m.w.Write(p)
	}
	m.buf.Write(p)
	if i := bytes.Index(m.buf.Bytes(), []byte(replMark)); i >= 0 {
		m.open = true
		rest := m.buf.Bytes()[i+len(replMark):]
		m.w.Write(rest)
	}
	return len(p), nil
}

var posPrefix = regexp.MustCompile(`^\S+\.go:\d+:\d+: `)

// showErrors prints build errors without the positions of the generated
// file, with the help of the diagnostics where there is one.
func (s *replSession) showErrors(err error, src string) {
	ds := diag.Process(err.Error(), diag.NewSources(), diag.Options{Dir: s.dir})
	seen := map[string]bool{}
	for _, d := range ds {
		msg := posPrefix.ReplaceAllString(d.Message, "")
		msg = strings.ReplaceAll(msg, "__fmt.", "fmt.")
		if seen[msg] || strings.Contains(msg, "__") {
			continue
		}
		seen[msg] = true
		fmt.Fprintln(os.Stderr, s.color("\x1b[1;31m", "error:")+" "+msg)
		for _, h := range d.Help {
			fmt.Fprintln(os.Stderr, s.color("\x1b[1;32m", "  help:")+" "+h)
		}
		if len(d.Fixes) > 0 {
			fmt.Fprintln(os.Stderr, s.color("\x1b[1;32m", "  help:")+" "+d.Fixes[0].Title)
		}
	}
}

var goKeywords = []string{"break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough",
	"for", "func", "go", "goto", "if", "import", "interface", "map", "package", "range", "return", "select",
	"struct", "switch", "type", "var"}

var lastIdent = regexp.MustCompile(`([A-Za-z_]\w*)(?:\.([A-Za-z_]\w*)?)?$`)

// suggest completes the identifier before the cursor: exported names after
// pkg., else session names, keywords, builtins and package names.
func (s *replSession) suggest(in string) string {
	m := lastIdent.FindStringSubmatch(in)
	if m == nil {
		return ""
	}
	full := m[0]
	if strings.Contains(full, ".") {
		pkg, prefix := m[1], m[2]
		path := ""
		for p, n := range s.imports {
			if n == pkg {
				path = p
			}
		}
		if path == "" {
			path, _ = s.src.StdImport(pkg, "")
		}
		if path == "" {
			return ""
		}
		return firstCompletion(prefix, s.src.Exports(path))
	}
	prefix := m[1]
	if len(prefix) < 2 {
		return ""
	}
	cands := append([]string{}, s.names...)
	for _, d := range s.decls {
		if f := strings.Fields(d); len(f) > 1 {
			cands = append(cands, strings.SplitN(f[1], "(", 2)[0])
		}
	}
	cands = append(cands, goKeywords...)
	cands = append(cands, "append", "cap", "clear", "close", "copy", "delete", "len", "make", "max", "min", "new", "panic", "print", "println", "recover")
	cands = append(cands, s.src.StdPackageNames()...)
	return firstCompletion(prefix, cands)
}

func firstCompletion(prefix string, cands []string) string {
	sort.Strings(cands)
	for _, c := range cands {
		if strings.HasPrefix(c, prefix) && c != prefix {
			return c[len(prefix):]
		}
	}
	return ""
}

// highlight colors Go tokens of one input line.
func (s *replSession) highlight(line string) string {
	if !s.colors {
		return line
	}
	h := &diag.Human{Style: diag.Colors}
	return h.Highlight(line)
}
