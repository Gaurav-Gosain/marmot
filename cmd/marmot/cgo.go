package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/cgo"
	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/Gaurav-Gosain/marmot/internal/driver"
)

type cgoFlags struct {
	goSrc  *bool
	cSrc   *bool
	output outputFlags
}

func newCgoFlags() (*flag.FlagSet, *cgoFlags) {
	fs := flag.NewFlagSet("cgo", flag.ContinueOnError)
	f := &cgoFlags{}
	f.goSrc = fs.Bool("go", false, "print the file's Go code as marmot compiles it")
	f.cSrc = fs.Bool("c", false, "print the file's C wrappers")
	f.output = addOutputFlags(fs)
	return fs, f
}

// cgoFunc is one C function a file calls.
type cgoFunc struct {
	Name  string `json:"name"`  // C.name
	Proto string `json:"proto"` // the wrapper's C prototype, with the C function's name
	Call  string `json:"call"`  // fast, bracketed, or helper (cgo's own Go code)
	Errno bool   `json:"errno"` // called as r, err := C.f(...)
	Uses  int    `json:"uses"`
}

type cgoReport struct {
	File      string    `json:"file"`
	Package   string    `json:"package"`
	OutDir    string    `json:"outDir"`
	Functions []cgoFunc `json:"functions"`
	Types     []string  `json:"types"`
	Vars      []string  `json:"vars"`
	Exports   []string  `json:"exports"`
	CFLAGS    []string  `json:"cflags"`
	LDFLAGS   []string  `json:"ldflags"`
}

var (
	reCgoUse     = regexp.MustCompile(`\b_C(func|2func|type|var|macro)_(\w+)`)
	reCgoWrapper = regexp.MustCompile(`(?m)^(?:CGO_NO_SANITIZE_THREAD\n)?([^\n(]+)\n_cgo_[0-9a-f]+_C(2?func)_(\w+)\(([^)]*)\)\n\{\n([^}]*)\}`)
	reExportLine = regexp.MustCompile(`(?m)^//export (\w+)`)
)

// cmdCgo implements marmot cgo show [-go] [-c] FILE.go.
func cmdCgo(args []string) int {
	fs, cf := newCgoFlags()
	fs.SetOutput(io.Discard)
	const usage = "marmot cgo show [-go] [-c] FILE.go"
	if len(args) == 0 || args[0] != "show" {
		rep := cf.output.reporter("cgo")
		help := []string{"the usage is `" + usage + "`"}
		if len(args) == 0 {
			return rep.Usage("missing subcommand `show`", help...)
		}
		return rep.Usage("unknown subcommand `"+args[0]+"` of `marmot cgo`", help...)
	}
	if err := fs.Parse(args[1:]); err != nil {
		return usageError("cgo", err)
	}
	rep := cf.output.reporter("cgo")
	if fs.NArg() != 1 {
		return argError(rep, "cgo", usage, fs.Args(), "FILE.go")
	}
	if !strings.HasSuffix(fs.Arg(0), ".go") {
		return rep.Usage("`"+fs.Arg(0)+"` is not a .go file", "the usage is `"+usage+"`")
	}
	file, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return rep.Usage("marmot cgo: " + err.Error())
	}
	r, goSrc, cSrc, err := cgoShow(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot cgo:", err)
		return 1
	}
	switch {
	case *cf.goSrc:
		fmt.Print(goSrc)
		return 0
	case *cf.cSrc:
		fmt.Print(cSrc)
		return 0
	case rep.Mode == diag.ModeJSON:
		e := json.NewEncoder(os.Stdout)
		e.SetEscapeHTML(false)
		_ = e.Encode(map[string]any{"schema": diag.Schema, "kind": "cgo", "report": r})
		return 0
	}
	st := diag.Plain
	if rep.Color || diag.IsTerminal(os.Stdout) && os.Getenv("NO_COLOR") == "" && rep.Mode == diag.ModeHuman {
		st = diag.Colors
	}
	printCgoReport(r, st)
	return 0
}

// cgoShow runs cgo on the package of file and reads what it made for the
// file: the rewritten Go, the C wrappers, and the report.
func cgoShow(file string) (*cgoReport, string, string, error) {
	if _, err := os.Stat(file); err != nil {
		return nil, "", "", err
	}
	root, err := driver.FindRoot()
	if err != nil {
		return nil, "", "", err
	}
	dir := filepath.Dir(file)
	// The file's package, or the file alone where the directory is not
	// one package (a directory of single-file programs: go list works,
	// cgo does not).
	path, files := "main", []string(nil)
	if out, err := exec.Command("go", "list", "-f", "{{.ImportPath}}", dir).Output(); err == nil {
		path = strings.TrimSpace(string(out))
	} else {
		files = []string{file}
	}
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return nil, "", "", fmt.Errorf("go env GOROOT: %v", err)
	}
	cfg := cgo.Config{CC: driver.LLVMTool("clang"), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Tags: []string{"marmot", "purego", "math_big_pure_go"}, RealGOROOT: strings.TrimSpace(string(goroot)),
		CacheDir: filepath.Join(root, "build", "cache", "cgo")}
	p, err := cgo.Prepare(cfg, dir, path, files)
	if err != nil && files == nil {
		p, err = cgo.Prepare(cfg, dir, path, []string{file})
	}
	if err != nil {
		return nil, "", "", err
	}
	if p == nil {
		return nil, "", "", fmt.Errorf("%s: the package has no cgo files", dir)
	}
	base := strings.TrimSuffix(filepath.Base(file), ".go")
	goData, err := os.ReadFile(filepath.Join(p.OutDir, base+".cgo1.go"))
	if err != nil {
		return nil, "", "", fmt.Errorf("%s does not import \"C\"", filepath.Base(file))
	}
	cData, _ := os.ReadFile(filepath.Join(p.OutDir, base+".cgo2.c"))
	r := &cgoReport{File: file, Package: path, OutDir: p.OutDir, CFLAGS: ownFlags(p.CFLAGS, dir, p.OutDir), LDFLAGS: p.LDFLAGS}

	uses := map[string]int{}
	kinds := map[string]map[string]bool{}
	for _, m := range reCgoUse.FindAllStringSubmatch(string(goData), -1) {
		k, n := m[1], m[2]
		if kinds[k] == nil {
			kinds[k] = map[string]bool{}
		}
		kinds[k][n] = true
		if k == "func" || k == "2func" {
			uses[n]++
		}
	}
	// Each C function's wrapper is in the .cgo2.c of a file that calls it.
	wrappers := map[string]cgoFunc{}
	cfiles, _ := filepath.Glob(filepath.Join(p.OutDir, "*.cgo2.c"))
	for _, cf := range cfiles {
		data, _ := os.ReadFile(cf)
		for _, m := range reCgoWrapper.FindAllStringSubmatch(string(data), -1) {
			ret, kind, name, params, body := strings.TrimSpace(m[1]), m[2], m[3], m[4], m[5]
			call := "bracketed"
			if strings.Contains(body, "rt_cgo_fast_enter") || strings.Contains(body, "rt_cgo_enter_fast") {
				call = "fast"
			}
			if params == "" {
				params = "void"
			}
			key := kind + " " + name
			if _, ok := wrappers[key]; !ok {
				wrappers[key] = cgoFunc{Name: "C." + name, Proto: fmt.Sprintf("%s %s(%s)", ret, name, params), Call: call, Errno: kind == "2func"}
			}
		}
	}
	for _, k := range []string{"func", "2func"} {
		for n := range kinds[k] {
			f, ok := wrappers[k+" "+n]
			if !ok {
				if h, ok := cgoHelpers[n]; ok {
					f = cgoFunc{Name: "C." + n, Proto: h, Call: "helper"}
				} else {
					f = cgoFunc{Name: "C." + n, Proto: n + "(...)", Call: "bracketed", Errno: k == "2func"}
				}
			}
			f.Uses = uses[n]
			r.Functions = append(r.Functions, f)
		}
	}
	sort.Slice(r.Functions, func(i, j int) bool {
		if r.Functions[i].Name != r.Functions[j].Name {
			return r.Functions[i].Name < r.Functions[j].Name
		}
		return !r.Functions[i].Errno
	})
	gotypes, _ := os.ReadFile(filepath.Join(p.OutDir, "_cgo_gotypes.go"))
	for n := range kinds["type"] {
		r.Types = append(r.Types, cgoTypeLine(string(gotypes), n))
	}
	sort.Strings(r.Types)
	for n := range kinds["var"] {
		r.Vars = append(r.Vars, "C."+n)
	}
	for n := range kinds["macro"] {
		r.Vars = append(r.Vars, "C."+n+" (a macro)")
	}
	sort.Strings(r.Vars)
	if h, err := os.ReadFile(filepath.Join(p.OutDir, "_cgo_export.h")); err == nil {
		// The file's //export functions, with the prototypes cgo wrote
		// (the last declaration: the header's prologue repeats the
		// package's own C).
		for _, m := range reExportLine.FindAllStringSubmatch(string(goData), -1) {
			re := regexp.MustCompile(`(?m)^extern ([^;\n(]*\b` + regexp.QuoteMeta(m[1]) + `\([^;\n]*\));`)
			if all := re.FindAllStringSubmatch(string(h), -1); len(all) > 0 {
				r.Exports = append(r.Exports, all[len(all)-1][1])
			} else {
				r.Exports = append(r.Exports, m[1])
			}
		}
	}
	// The -go output ends with the declarations of the C functions the
	// file calls: the direct C call (a linkname) and its Go wrapper.
	var decls []string
	for _, f := range r.Functions {
		if f.Call == "helper" {
			continue
		}
		k := "Cfunc_"
		if f.Errno {
			k = "C2func_"
		}
		n := strings.TrimPrefix(f.Name, "C.")
		re := regexp.MustCompile(`(?s)//go:linkname _cgo_[0-9a-f]+_` + k + regexp.QuoteMeta(n) + ` .*?\nfunc _` + k + regexp.QuoteMeta(n) + `\(.*?\n}\n`)
		if m := re.FindString(string(gotypes)); m != "" {
			decls = append(decls, m)
		}
	}
	goOut := string(goData)
	if len(decls) > 0 {
		goOut += "\n// The C calls of this file, from _cgo_gotypes.go:\n\n" + strings.Join(decls, "\n")
	}
	return r, goOut, string(cData), nil
}

// cgoHelpers are cgo's own functions: Go code, not a C call.
var cgoHelpers = map[string]string{
	"CString":   "C.CString(string) *C.char: copies the string into C memory (free it)",
	"CBytes":    "C.CBytes([]byte) unsafe.Pointer: copies the bytes into C memory (free it)",
	"GoString":  "C.GoString(*C.char) string: copies a C string",
	"GoStringN": "C.GoStringN(*C.char, C.int) string: copies n bytes",
	"GoBytes":   "C.GoBytes(unsafe.Pointer, C.int) []byte: copies n bytes",
	"_CMalloc":  "C.malloc(C.size_t) unsafe.Pointer: C memory, never nil",
}

// ownFlags drops the include flags cgo adds itself (the package and its
// output directory).
func ownFlags(flags []string, dir, out string) []string {
	var keep []string
	for i := 0; i < len(flags); i++ {
		if flags[i] == "-I" && i+1 < len(flags) && (flags[i+1] == dir || flags[i+1] == out) {
			i++
			continue
		}
		keep = append(keep, flags[i])
	}
	return keep
}

// cgoTypeLine is a C type's line: C.name and its Go definition.
func cgoTypeLine(gotypes, n string) string {
	re := regexp.MustCompile(`(?m)^type _Ctype_` + regexp.QuoteMeta(n) + ` (=?\s*.*)$`)
	m := re.FindStringSubmatch(gotypes)
	name := "C." + n
	if m == nil {
		return name
	}
	def := strings.TrimSpace(m[1])
	if strings.HasSuffix(def, "{") {
		// A struct: its fields up to the closing brace.
		start := strings.Index(gotypes, m[0]) + len(m[0])
		end := strings.Index(gotypes[start:], "\n}")
		if end >= 0 {
			var fields []string
			for _, l := range strings.Split(gotypes[start:start+end], "\n") {
				if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "_ ") {
					fields = append(fields, cgoDemangle(l))
				}
			}
			def = "struct { " + strings.Join(fields, "; ") + " }"
		}
	}
	if strings.HasPrefix(def, "=") {
		target := strings.TrimSpace(def[1:])
		if strings.HasPrefix(target, "_Ctype_struct___") {
			// An anonymous struct behind a typedef: show its fields.
			return name + " = " + strings.TrimPrefix(cgoTypeLine(gotypes, strings.TrimPrefix(target, "_Ctype_")), "C."+strings.TrimPrefix(target, "_Ctype_")+" ")
		}
		return name + " = " + cgoDemangle(target)
	}
	return name + " " + cgoDemangle(def)
}

var reCgoMangled = regexp.MustCompile(`_C(?:type|func|var)_(\w+)`)

func cgoDemangle(s string) string { return reCgoMangled.ReplaceAllString(s, "C.$1") }

func printCgoReport(r *cgoReport, st diag.Style) {
	rel := r.File
	if wd, err := os.Getwd(); err == nil {
		if p, err := filepath.Rel(wd, r.File); err == nil && !strings.HasPrefix(p, "..") {
			rel = p
		}
	}
	fmt.Printf("%s%s%s (package %s): %d C functions, %d C types, %d C variables, %d exports\n",
		st.Bold, rel, st.Reset, r.Package, len(r.Functions), len(r.Types), len(r.Vars), len(r.Exports))
	if len(r.Functions) > 0 {
		fmt.Printf("\n%sC functions%s\n", st.Bold, st.Reset)
		w := 0
		for _, f := range r.Functions {
			w = max(w, len(f.Proto))
		}
		w = min(w, 60)
		for _, f := range r.Functions {
			how := "fast: calls nothing, no scheduler hand-off"
			switch f.Call {
			case "bracketed":
				how = "bracketed: releases its P while C runs (it may block or call Go)"
			case "helper":
				how = "cgo helper (Go code)"
			}
			if f.Errno {
				how += "; keeps errno (r, err := " + f.Name + "(...))"
			}
			fmt.Printf("  %-*s  %s%s%s\n", w, f.Proto, st.Dim, how, st.Reset)
		}
	}
	if len(r.Types) > 0 {
		fmt.Printf("\n%sC types%s\n", st.Bold, st.Reset)
		for _, t := range r.Types {
			fmt.Println("  " + t)
		}
	}
	if len(r.Vars) > 0 {
		fmt.Printf("\n%sC variables%s\n", st.Bold, st.Reset)
		for _, v := range r.Vars {
			fmt.Println("  " + v)
		}
	}
	if len(r.Exports) > 0 {
		fmt.Printf("\n%sexports%s (C calls Go)\n", st.Bold, st.Reset)
		for _, e := range r.Exports {
			fmt.Println("  " + e)
		}
	}
	if len(r.CFLAGS) > 0 || len(r.LDFLAGS) > 0 {
		fmt.Printf("\n%s#cgo flags%s\n", st.Bold, st.Reset)
		if len(r.CFLAGS) > 0 {
			fmt.Println("  CFLAGS  " + strings.Join(r.CFLAGS, " "))
		}
		if len(r.LDFLAGS) > 0 {
			fmt.Println("  LDFLAGS " + strings.Join(r.LDFLAGS, " "))
		}
	}
	fmt.Printf("\n%s(marmot cgo show -go FILE prints the Go code as compiled, -c the C wrappers)%s\n", st.Dim, st.Reset)
}
