// Command editfuzz checks incremental builds against clean builds under
// edits. It copies a multi-package program (its vendored dependencies and,
// with -goroot, a copy of the standard library included), applies random
// semantic edits one at a time, and after each edit builds incrementally
// and from a clean cache. Both builds must succeed or fail together, the
// programs must print the same and exit the same, and every module of the
// clean build must be byte for byte the incremental build's module under
// the same key.
//
//	go run ./tests/editfuzz -prog tests/world/goldmark,tests/world/lru -n 300 -seed 1
//	go run ./tests/editfuzz -prog self -sweep -goroot /tmp/goroot-copy -run version
//
// -prog self is cmd/marmot (the committed tree, dependencies vendored).
// -sweep makes one body edit and one API edit in every editable package
// instead of random edits. An edit that stock Go rejects is undone and
// another is tried. Failures are kept under -work with the edit's diff.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	flProg   = flag.String("prog", "tests/world/goldmark", "comma list of program directories (modules), or self for cmd/marmot")
	flN      = flag.Int("n", 50, "random edits per program")
	flSeed   = flag.Int64("seed", 1, "random seed")
	flSweep  = flag.Bool("sweep", false, "one body edit and one API edit in every editable package instead of random edits")
	flMarmot = flag.String("marmot", "build/bin/marmot", "marmot binary")
	flWork   = flag.String("work", ".work/editfuzz", "work directory (emptied first)")
	flGoroot = flag.String("goroot", "", "a writable copy of GOROOT: the standard library becomes editable too")
	flRun    = flag.String("run", "", "arguments for the built program (space separated)")
	flMax    = flag.Int("maxfail", 10, "stop a program after this many failures")
)

func main() {
	flag.Parse()
	repo, _ := os.Getwd()
	marmot, _ := filepath.Abs(*flMarmot)
	work, _ := filepath.Abs(*flWork)
	if err := os.RemoveAll(work); err != nil {
		fatal(err)
	}
	total, failed := 0, 0
	for i, prog := range strings.Split(*flProg, ",") {
		f := &fuzzer{repo: repo, marmot: marmot, dir: filepath.Join(work, fmt.Sprint(i)), r: rand.New(rand.NewSource(*flSeed + int64(i)))}
		n, bad := f.runProgram(prog)
		total += n
		failed += bad
	}
	fmt.Printf("editfuzz: %d edits checked, %d failures\n", total, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "editfuzz:", err)
	os.Exit(2)
}

type pkg struct {
	path, name, dir string
	files           []string
	imports         []string
}

type fuzzer struct {
	repo, marmot, dir string
	prog, root        string // the program copy and the marmot root
	pristine          string
	r                 *rand.Rand
	pkgs              []*pkg
	byPath            map[string]*pkg
	n                 int // unique suffix for added names
	failures          int
}

func (f *fuzzer) runProgram(src string) (checked, failed int) {
	f.prog = filepath.Join(f.dir, "prog")
	f.root = filepath.Join(f.dir, "root")
	f.pristine = filepath.Join(f.dir, "pristine")
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		fatal(err)
	}
	if src == "self" {
		if out, err := sh(f.repo, nil, "sh", "-c", "mkdir -p "+f.prog+" && git archive HEAD | tar -x -C "+f.prog); err != nil {
			fatal(fmt.Errorf("copy the tree: %v\n%s", err, out))
		}
		// -e: packages of the module that cmd/marmot does not import may
		// need modules that are not in the cache; they are left out.
		if out, err := sh(f.prog, []string{"GOFLAGS=-mod=mod", "GOPROXY=off"}, "go", "mod", "vendor", "-e"); err != nil {
			fatal(fmt.Errorf("go mod vendor: %v\n%s", err, out))
		}
		src = "cmd/marmot"
	} else {
		copyTree(filepath.Join(f.repo, src), f.prog)
		src = "."
	}
	copyTree(f.prog, f.pristine)
	// The root: lib is copied (small), the tools are the repository's.
	copyTree(filepath.Join(f.repo, "lib"), filepath.Join(f.root, "lib"))
	os.MkdirAll(filepath.Join(f.root, "build"), 0o755)
	for _, l := range []string{"build/bin", "build/lib"} {
		os.Symlink(filepath.Join(f.repo, l), filepath.Join(f.root, l))
	}
	main := filepath.Join(f.prog, src)
	f.listPackages(main)
	fmt.Printf("== %s: %d editable packages\n", src, len(f.pkgs))
	if res := f.compare(main, "before any edit"); res != "" {
		fmt.Printf("FAIL before any edit: %s\n", res)
		return 1, 1
	}
	if *flSweep {
		for _, p := range f.pkgs {
			for _, m := range []mutation{mutBody, mutAddFunc} {
				if f.failures >= *flMax {
					break
				}
				if desc, ok := f.applyIn(main, p, m); ok {
					checked++
					f.check(main, desc)
				}
			}
		}
	} else {
		for checked < *flN && f.failures < *flMax {
			p := f.pkgs[f.r.Intn(len(f.pkgs))]
			m := mutations[f.r.Intn(len(mutations))]
			if desc, ok := f.applyIn(main, p, m); ok {
				checked++
				f.check(main, desc)
			}
		}
	}
	return checked, f.failures
}

// listPackages finds the editable packages of the program in dir: its
// own, the vendored ones and, with -goroot, the standard library's.
func (f *fuzzer) listPackages(dir string) {
	out, err := sh(dir, f.goEnv(), "go", "list", "-deps", "-f", "{{.ImportPath}}\t{{.Name}}\t{{.Dir}}\t{{join .GoFiles \",\"}}\t{{join .Imports \",\"}}", ".")
	if err != nil {
		fatal(fmt.Errorf("go list: %v\n%s", err, out))
	}
	f.byPath = map[string]*pkg{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fs := strings.Split(line, "\t")
		if len(fs) < 5 || fs[3] == "" {
			continue
		}
		editable := strings.HasPrefix(fs[2], f.prog+string(os.PathSeparator)) || fs[2] == f.prog ||
			(*flGoroot != "" && strings.HasPrefix(fs[2], *flGoroot+string(os.PathSeparator)) && fs[0] != "unsafe")
		if !editable {
			continue
		}
		p := &pkg{path: fs[0], name: fs[1], dir: fs[2]}
		for _, g := range strings.Split(fs[3], ",") {
			p.files = append(p.files, filepath.Join(fs[2], g))
		}
		if fs[4] != "" {
			p.imports = strings.Split(fs[4], ",")
		}
		f.pkgs = append(f.pkgs, p)
		f.byPath[p.path] = p
	}
	sort.Slice(f.pkgs, func(i, j int) bool { return f.pkgs[i].path < f.pkgs[j].path })
}

func (f *fuzzer) goEnv() []string {
	env := []string{"CGO_ENABLED=0", "GOFLAGS=-mod=vendor", "GOTOOLCHAIN=local"}
	if *flGoroot != "" {
		env = append(env, "GOROOT="+*flGoroot)
	}
	return env
}

func (f *fuzzer) marmotEnv() []string {
	return append(f.goEnv(), "MARMOT_ROOT="+f.root, "MARMOT_DAEMON=0", "MARMOT_TIER1=0", "MARMOT_OUTPUT=plain", "MARMOT_CACHE_MAX_MB=8192", "MARMOT_INCREMENTAL=")
}

// applyIn tries mutation m in package p up to a few times; an edit stock Go
// rejects is undone.
func (f *fuzzer) applyIn(main string, p *pkg, m mutation) (string, bool) {
	for try := 0; try < 6; try++ {
		saved := f.snapshot(p)
		desc, ok := m(f, p)
		if !ok {
			f.restore(saved)
			continue
		}
		if out, err := sh(main, f.goEnv(), "go", "build", "-o", filepath.Join(f.dir, "gocheck"), "."); err != nil {
			_ = out
			f.restore(saved)
			continue
		}
		return desc, true
	}
	return "", false
}

// check compares an incremental build after the edit with a clean one.
func (f *fuzzer) check(main, desc string) {
	if res := f.compare(main, desc); res != "" {
		f.failures++
		fmt.Printf("FAIL %s: %s\n", desc, res)
		dir := filepath.Join(f.dir, fmt.Sprintf("failure-%d", f.failures))
		os.MkdirAll(dir, 0o755)
		diff, _ := sh(f.dir, nil, "diff", "-ruN", f.pristine, f.prog)
		os.WriteFile(filepath.Join(dir, "edit.diff"), []byte(diff), 0o644)
		os.WriteFile(filepath.Join(dir, "why.txt"), []byte(desc+"\n"+res+"\n"), 0o644)
		return
	}
	fmt.Printf("ok   %s\n", desc)
}

func (f *fuzzer) compare(main, _ string) string {
	cache := filepath.Join(f.root, "build", "cache")
	incr := filepath.Join(f.root, "build", "cache.incr")
	incOut, incErr := f.buildRun(main, filepath.Join(f.dir, "inc.exe"))
	os.RemoveAll(incr)
	if err := os.Rename(cache, incr); err != nil {
		return "no cache after the incremental build: " + err.Error()
	}
	defer func() {
		os.RemoveAll(cache)
		os.Rename(incr, cache)
	}()
	cleanOut, cleanErr := f.buildRun(main, filepath.Join(f.dir, "clean.exe"))
	if (incErr == nil) != (cleanErr == nil) {
		return fmt.Sprintf("incremental build: %v; clean build: %v", incErr, cleanErr)
	}
	if incOut != cleanOut {
		return fmt.Sprintf("output differs\n--- incremental\n%s\n--- clean\n%s", incOut, cleanOut)
	}
	mods, _ := filepath.Glob(filepath.Join(cache, "mlir", "*.mlir"))
	for _, m := range mods {
		want, _ := os.ReadFile(m)
		got, err := os.ReadFile(filepath.Join(incr, "mlir", filepath.Base(m)))
		if err != nil {
			return "module " + filepath.Base(m) + " of the clean build is not in the incremental cache"
		}
		if !bytes.Equal(got, want) {
			return "module " + filepath.Base(m) + " differs"
		}
	}
	return ""
}

// buildRun builds the program and returns what running it shows, or the
// build error.
func (f *fuzzer) buildRun(main, exe string) (string, error) {
	os.Remove(exe)
	if out, err := sh(main, f.marmotEnv(), f.marmot, "build", "-o", exe, "."); err != nil {
		return "", fmt.Errorf("%v: %s", err, lastLines(out, 8))
	}
	out, err := sh(main, nil, exe, strings.Fields(*flRun)...)
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\nexit %d", out, code), nil
}

type saved map[string][]byte

func (f *fuzzer) snapshot(p *pkg) saved {
	s := saved{}
	for _, q := range f.pkgs {
		if q == p || contains(q.imports, p.path) {
			for _, file := range q.files {
				data, _ := os.ReadFile(file)
				s[file] = data
			}
		}
	}
	return s
}

func (f *fuzzer) restore(s saved) {
	for file, data := range s {
		os.WriteFile(file, data, 0o644)
	}
}

// Mutations change package p (and for some its importers) and describe
// the edit, or report false when they find nothing to change.
type mutation func(f *fuzzer, p *pkg) (string, bool)

var mutations = []mutation{mutLiteral, mutBody, mutAddFunc, mutRemoveFunc, mutAddInstance, mutRemoveInstance, mutMethod, mutField, mutMove, mutConst}

func (f *fuzzer) pickFile(p *pkg) (string, *token.FileSet, *ast.File, bool) {
	file := p.files[f.r.Intn(len(p.files))]
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		return "", nil, nil, false
	}
	return file, fset, af, true
}

func write(file string, fset *token.FileSet, af *ast.File) bool {
	var b bytes.Buffer
	if err := format.Node(&b, fset, af); err != nil {
		return false
	}
	return os.WriteFile(file, b.Bytes(), 0o644) == nil
}

func funcs(af *ast.File) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	for _, d := range af.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
			out = append(out, fd)
		}
	}
	return out
}

func rel(f *fuzzer, file string) string {
	if r, err := filepath.Rel(f.prog, file); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return file
}

func mutLiteral(f *fuzzer, p *pkg) (string, bool) {
	file, fset, af, ok := f.pickFile(p)
	if !ok {
		return "", false
	}
	var lits []*ast.BasicLit
	for _, fd := range funcs(af) {
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if l, ok := n.(*ast.BasicLit); ok && (l.Kind == token.INT || l.Kind == token.STRING && strings.HasPrefix(l.Value, `"`)) {
				lits = append(lits, l)
			}
			return true
		})
	}
	if len(lits) == 0 {
		return "", false
	}
	l := lits[f.r.Intn(len(lits))]
	old := l.Value
	if l.Kind == token.INT {
		v, err := strconv.ParseInt(l.Value, 10, 64)
		if err != nil {
			return "", false
		}
		l.Value = strconv.FormatInt(v+1, 10)
	} else {
		l.Value = l.Value[:len(l.Value)-1] + `z"`
	}
	return fmt.Sprintf("literal %s -> %s in %s", old, l.Value, rel(f, file)), write(file, fset, af)
}

func mutBody(f *fuzzer, p *pkg) (string, bool) {
	file, fset, af, ok := f.pickFile(p)
	if !ok {
		return "", false
	}
	fs := funcs(af)
	if len(fs) == 0 {
		return "", false
	}
	fd := fs[f.r.Intn(len(fs))]
	stmt := &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("_")}, Tok: token.ASSIGN, Rhs: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "0"}}}
	fd.Body.List = append([]ast.Stmt{stmt}, fd.Body.List...)
	return fmt.Sprintf("body line in %s (%s)", fd.Name.Name, rel(f, file)), write(file, fset, af)
}

func (f *fuzzer) appendDecl(p *pkg, src string) (string, bool) {
	file := p.files[f.r.Intn(len(p.files))]
	data, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	return file, os.WriteFile(file, append(data, []byte("\n"+src+"\n")...), 0o644) == nil
}

func mutAddFunc(f *fuzzer, p *pkg) (string, bool) {
	f.n++
	name := fmt.Sprintf("zzFunc%d", f.n)
	if f.r.Intn(2) == 0 {
		name = fmt.Sprintf("ZzFunc%d", f.n)
	}
	file, ok := f.appendDecl(p, fmt.Sprintf("func %s() int { return %d }", name, f.n))
	return fmt.Sprintf("add func %s.%s (%s)", p.path, name, rel(f, file)), ok
}

func mutRemoveFunc(f *fuzzer, p *pkg) (string, bool) {
	file, fset, af, ok := f.pickFile(p)
	if !ok {
		return "", false
	}
	var cands []int
	for i, d := range af.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name != "main" && fd.Name.Name != "init" {
			cands = append(cands, i)
		}
	}
	if len(cands) == 0 {
		return "", false
	}
	i := cands[f.r.Intn(len(cands))]
	name := af.Decls[i].(*ast.FuncDecl).Name.Name
	af.Decls = append(af.Decls[:i], af.Decls[i+1:]...)
	return fmt.Sprintf("remove func %s.%s (%s)", p.path, name, rel(f, file)), write(file, fset, af)
}

// mutAddInstance adds an exported generic function to p and a new
// instantiation of it in a package that imports p.
func mutAddInstance(f *fuzzer, p *pkg) (string, bool) {
	if p.name == "main" {
		return "", false
	}
	var importers []*pkg
	for _, q := range f.pkgs {
		if contains(q.imports, p.path) {
			importers = append(importers, q)
		}
	}
	if len(importers) == 0 {
		return "", false
	}
	q := importers[f.r.Intn(len(importers))]
	f.n++
	name := fmt.Sprintf("ZzGen%d", f.n)
	if _, ok := f.appendDecl(p, fmt.Sprintf("func %s[T any](x T) T { return x }", name)); !ok {
		return "", false
	}
	vals := []string{"1.5", `"s"`, "int8(3)", "struct{ zz int }{}", "[]byte(nil)", "uint16(7)"}
	val := vals[f.r.Intn(len(vals))]
	for _, file := range q.files {
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		local := ""
		for _, im := range af.Imports {
			if path, _ := strconv.Unquote(im.Path.Value); path == p.path {
				local = p.name
				if im.Name != nil {
					local = im.Name.Name
				}
			}
		}
		fs := funcs(af)
		if local == "" || local == "_" || local == "." || len(fs) == 0 {
			continue
		}
		fd := fs[f.r.Intn(len(fs))]
		call, err := parser.ParseExpr(fmt.Sprintf("%s.%s(%s)", local, name, val))
		if err != nil {
			return "", false
		}
		stmt := &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("_")}, Tok: token.ASSIGN, Rhs: []ast.Expr{call}}
		fd.Body.List = append([]ast.Stmt{stmt}, fd.Body.List...)
		return fmt.Sprintf("instantiate %s.%s(%s) in %s.%s", p.path, name, val, q.path, fd.Name.Name), write(file, fset, af)
	}
	return "", false
}

// mutRemoveInstance removes an instantiation an earlier edit added.
func mutRemoveInstance(f *fuzzer, p *pkg) (string, bool) {
	for _, file := range p.files {
		fset := token.NewFileSet()
		af, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		for _, fd := range funcs(af) {
			for i, s := range fd.Body.List {
				as, ok := s.(*ast.AssignStmt)
				if !ok || len(as.Rhs) != 1 {
					continue
				}
				if c, ok := as.Rhs[0].(*ast.CallExpr); ok {
					if sel, ok := c.Fun.(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "ZzGen") {
						fd.Body.List = append(fd.Body.List[:i], fd.Body.List[i+1:]...)
						return fmt.Sprintf("remove instantiation of %s in %s.%s", sel.Sel.Name, p.path, fd.Name.Name), write(file, fset, af)
					}
				}
			}
		}
	}
	return "", false
}

func structTypes(af *ast.File) []*ast.TypeSpec {
	var out []*ast.TypeSpec
	for _, d := range af.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			ts := s.(*ast.TypeSpec)
			if _, ok := ts.Type.(*ast.StructType); ok && ts.TypeParams == nil && !ts.Assign.IsValid() {
				out = append(out, ts)
			}
		}
	}
	return out
}

func mutMethod(f *fuzzer, p *pkg) (string, bool) {
	_, _, af, ok := f.pickFile(p)
	if !ok {
		return "", false
	}
	ts := structTypes(af)
	if len(ts) == 0 {
		return "", false
	}
	t := ts[f.r.Intn(len(ts))].Name.Name
	f.n++
	m := fmt.Sprintf("ZzMethod%d", f.n)
	file, ok := f.appendDecl(p, fmt.Sprintf("func (*%s) %s() int { return %d }", t, m, f.n))
	return fmt.Sprintf("add method %s.(*%s).%s (%s)", p.path, t, m, rel(f, file)), ok
}

func mutField(f *fuzzer, p *pkg) (string, bool) {
	file, fset, af, ok := f.pickFile(p)
	if !ok {
		return "", false
	}
	ts := structTypes(af)
	if len(ts) == 0 {
		return "", false
	}
	t := ts[f.r.Intn(len(ts))]
	f.n++
	st := t.Type.(*ast.StructType)
	st.Fields.List = append(st.Fields.List, &ast.Field{Names: []*ast.Ident{ast.NewIdent(fmt.Sprintf("zzField%d", f.n))}, Type: ast.NewIdent("int")})
	return fmt.Sprintf("add field to %s.%s (%s)", p.path, t.Name.Name, rel(f, file)), write(file, fset, af)
}

func mutMove(f *fuzzer, p *pkg) (string, bool) {
	if len(p.files) < 2 {
		return "", false
	}
	i, j := f.r.Intn(len(p.files)), f.r.Intn(len(p.files)-1)
	if j >= i {
		j++
	}
	from, to := p.files[i], p.files[j]
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, from, nil, parser.ParseComments)
	if err != nil {
		return "", false
	}
	var cands []int
	for k, d := range af.Decls {
		if _, ok := d.(*ast.FuncDecl); ok {
			cands = append(cands, k)
		}
	}
	if len(cands) == 0 {
		return "", false
	}
	k := cands[f.r.Intn(len(cands))]
	fd := af.Decls[k].(*ast.FuncDecl)
	var b bytes.Buffer
	if err := format.Node(&b, fset, fd); err != nil {
		return "", false
	}
	af.Decls = append(af.Decls[:k], af.Decls[k+1:]...)
	af.Comments = nil // the moved function's comments would stay behind
	if !write(from, fset, af) {
		return "", false
	}
	data, err := os.ReadFile(to)
	if err != nil {
		return "", false
	}
	ok := os.WriteFile(to, append(data, append([]byte("\n"), b.Bytes()...)...), 0o644) == nil
	return fmt.Sprintf("move %s from %s to %s", fd.Name.Name, rel(f, from), rel(f, to)), ok
}

func mutConst(f *fuzzer, p *pkg) (string, bool) {
	file, fset, af, ok := f.pickFile(p)
	if !ok {
		return "", false
	}
	var lits []*ast.BasicLit
	for _, d := range af.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			for _, v := range s.(*ast.ValueSpec).Values {
				if l, ok := v.(*ast.BasicLit); ok && l.Kind == token.INT {
					lits = append(lits, l)
				}
			}
		}
	}
	if len(lits) == 0 {
		return "", false
	}
	l := lits[f.r.Intn(len(lits))]
	v, err := strconv.ParseInt(l.Value, 10, 64)
	if err != nil {
		return "", false
	}
	old := l.Value
	l.Value = strconv.FormatInt(v+1, 10)
	return fmt.Sprintf("const %s -> %s in %s", old, l.Value, rel(f, file)), write(file, fset, af)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func sh(dir string, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	return b.String(), err
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func copyTree(from, to string) {
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		r, _ := filepath.Rel(from, path)
		dst := filepath.Join(to, r)
		if info.Mode()&os.ModeSymlink != 0 {
			t, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(t, dst)
		}
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		fatal(err)
	}
}
