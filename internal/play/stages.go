package play

import (
	"fmt"
	"go/token"
	"go/types"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ssa"

	"github.com/Gaurav-Gosain/marmot/internal/binsize"
	"github.com/Gaurav-Gosain/marmot/internal/driver"
	"github.com/Gaurav-Gosain/marmot/internal/loader"
)

// Each stage turns marmot's output into display text and, per text line,
// the line of main.go it comes from: SSA from the instructions'
// positions, MLIR from loc() attributes, LLVM IR from !dbg locations (an
// inlined call's location is the call site in main.go), machine code from
// the object's line table.

// ssaText is package main's SSA, function by function in source order.
func ssaText(root, dir string) (string, []int, error) {
	lp, err := loader.Load(loader.Config{Dir: dir, Root: root, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Patterns: []string{"."},
		CC: driver.LLVMTool("clang")})
	if err != nil {
		return "", nil, err
	}
	var pkg *ssa.Package
	for _, p := range lp.Packages {
		if p.Name == "main" {
			pkg = lp.SSAPkgs[p.Types]
		}
	}
	if pkg == nil {
		return "", nil, fmt.Errorf("no package main")
	}
	fset := pkg.Prog.Fset
	var funcs []*ssa.Function
	var add func(f *ssa.Function)
	add = func(f *ssa.Function) {
		funcs = append(funcs, f)
		for _, a := range f.AnonFuncs {
			add(a)
		}
	}
	for _, m := range pkg.Members {
		if f, ok := m.(*ssa.Function); ok {
			add(f)
		}
		if t, ok := m.(*ssa.Type); ok {
			ms := pkg.Prog.MethodSets.MethodSet(t.Type())
			pms := pkg.Prog.MethodSets.MethodSet(types.NewPointer(t.Type()))
			for _, set := range [][]*ssa.Function{methodsOf(pkg.Prog, ms), methodsOf(pkg.Prog, pms)} {
				for _, f := range set {
					if f != nil && f.Pkg == pkg && f.Synthetic == "" {
						add(f)
					}
				}
			}
		}
	}
	seen := map[*ssa.Function]bool{}
	var uniq []*ssa.Function
	for _, f := range funcs {
		if !seen[f] && len(f.Blocks) > 0 {
			seen[f] = true
			uniq = append(uniq, f)
		}
	}
	sort.SliceStable(uniq, func(i, j int) bool { return uniq[i].Pos() < uniq[j].Pos() })
	main := filepath.Join(dir, "main.go")
	lineOf := func(p token.Pos) int {
		if !p.IsValid() {
			return 0
		}
		pos := fset.Position(p)
		if pos.Filename != main {
			return 0
		}
		return pos.Line
	}
	var b strings.Builder
	var lines []int
	emit := func(s string, l int) {
		b.WriteString(s)
		b.WriteByte('\n')
		lines = append(lines, l)
	}
	for i, f := range uniq {
		if i > 0 {
			emit("", 0)
		}
		emit(fmt.Sprintf("func %s%s", f.RelString(pkg.Pkg), strings.TrimPrefix(f.Signature.String(), "func")), lineOf(f.Pos()))
		for _, bl := range f.Blocks {
			emit(fmt.Sprintf("%d:  %s", bl.Index, bl.Comment), 0)
			for _, in := range bl.Instrs {
				s := in.String()
				if v, ok := in.(ssa.Value); ok && v.Name() != "" {
					s = v.Name() + " = " + s
				}
				emit("\t"+s, lineOf(in.Pos()))
			}
		}
	}
	return b.String(), lines, nil
}

func methodsOf(prog *ssa.Program, ms *types.MethodSet) []*ssa.Function {
	var out []*ssa.Function
	for i := range ms.Len() {
		out = append(out, prog.MethodValue(ms.At(i)))
	}
	return out
}

var (
	reLocDef  = regexp.MustCompile(`^#(loc\d*) = loc\((.*)\)$`)
	reLocUse  = regexp.MustCompile(` ?loc\((#loc\d*|"[^"]*":\d+:\d+)\)`)
	reLocFile = regexp.MustCompile(`^"([^"]*)":(\d+):\d+`)
	reLocRef  = regexp.MustCompile(`#loc\d*`)
)

// mlirLines drops the loc() attributes and #loc definitions from the
// MLIR text and maps each operation to its line of main.go.
func mlirLines(text, main string) (string, []int) {
	defs := map[string]string{}
	for _, l := range strings.Split(text, "\n") {
		if m := reLocDef.FindStringSubmatch(l); m != nil {
			defs["#"+m[1]] = m[2]
		}
	}
	var resolve func(s string, depth int) int
	resolve = func(s string, depth int) int {
		if depth > 20 {
			return 0
		}
		if m := reLocFile.FindStringSubmatch(s); m != nil {
			if m[1] == main {
				n, _ := strconv.Atoi(m[2])
				return n
			}
			return 0
		}
		if strings.HasPrefix(s, "#loc") {
			return resolve(defs[s], depth+1)
		}
		// callsite(#callee at #caller), fused[...]: the first part in
		// main.go, the caller last (an inlined callee's line is not
		// main.go's).
		for _, r := range reLocRef.FindAllString(s, -1) {
			if n := resolve(r, depth+1); n > 0 {
				return n
			}
		}
		if m := reLocFile.FindStringSubmatch(strings.TrimLeft(s, "callsitefused[(")); m != nil {
			return resolve(m[0], depth+1)
		}
		return 0
	}
	var out []string
	var lines []int
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "#loc") {
			continue
		}
		n := 0
		if m := reLocUse.FindAllStringSubmatch(l, -1); m != nil {
			n = resolve(m[len(m)-1][1], 0)
		}
		out = append(out, reLocUse.ReplaceAllString(l, ""))
		lines = append(lines, n)
	}
	return trimTrailing(out, lines)
}

var (
	reDbgUse = regexp.MustCompile(`, !dbg (!\d+)`)
	reMeta   = regexp.MustCompile(`^(!\d+) = (?:distinct )?!(\w+)\((.*)\)$`)
	reField  = regexp.MustCompile(`(\w+): ("(?:[^"\\]|\\.)*"|![\d]+|[\w.-]+)`)
	reDrop   = regexp.MustCompile(`, !(?:llvm\.loop|tbaa|range|noundef|nonnull|prof|srcloc|annotation) !\d+`)
)

// llvmLines drops the debug metadata from the LLVM IR and maps each
// instruction with a location to its line of main.go.
func llvmLines(text, main string) (string, []int) {
	type node struct {
		kind   string
		fields map[string]string
	}
	meta := map[string]node{}
	for _, l := range strings.Split(text, "\n") {
		if m := reMeta.FindStringSubmatch(l); m != nil {
			n := node{m[2], map[string]string{}}
			for _, f := range reField.FindAllStringSubmatch(m[3], -1) {
				n.fields[f[1]] = f[2]
			}
			meta[m[1]] = n
		}
	}
	unq := func(s string) string { v, _ := strconv.Unquote(s); return v }
	isMain := func(scope string) bool {
		for range 50 {
			n, ok := meta[scope]
			if !ok {
				return false
			}
			if f, ok := n.fields["file"]; ok {
				fn := meta[f]
				name := unq(fn.fields["filename"])
				if !filepath.IsAbs(name) {
					name = filepath.Join(unq(fn.fields["directory"]), name)
				}
				return name == main
			}
			scope = n.fields["scope"]
		}
		return false
	}
	var lineOf func(id string, depth int) int
	lineOf = func(id string, depth int) int {
		n, ok := meta[id]
		if !ok || n.kind != "DILocation" || depth > 50 {
			return 0
		}
		if isMain(n.fields["scope"]) {
			v, _ := strconv.Atoi(n.fields["line"])
			return v
		}
		if at, ok := n.fields["inlinedAt"]; ok {
			return lineOf(at, depth+1)
		}
		return 0
	}
	var out []string
	var lines []int
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "!") || strings.HasPrefix(l, "attributes #") || strings.HasPrefix(l, "source_filename") || strings.HasPrefix(l, "; ModuleID") {
			continue
		}
		n := 0
		if m := reDbgUse.FindStringSubmatch(l); m != nil {
			n = lineOf(m[1], 0)
		} else if strings.HasPrefix(l, "define ") {
			if m := regexp.MustCompile(`!dbg (!\d+)`).FindStringSubmatch(l); m != nil {
				if sp, ok := meta[m[1]]; ok && isMain(m[1]) {
					n, _ = strconv.Atoi(sp.fields["line"])
				}
			}
		}
		l = reDbgUse.ReplaceAllString(l, "")
		l = regexp.MustCompile(` !dbg !\d+`).ReplaceAllString(l, "")
		l = reDrop.ReplaceAllString(l, "")
		out = append(out, l)
		lines = append(lines, n)
	}
	return trimTrailing(out, lines)
}

var reObjLine = regexp.MustCompile(`^; (.+):(\d+)$`)

// asmLines disassembles the object with its line table.
func asmLines(obj, main string) (string, []int, error) {
	out, err := exec.Command(driver.LLVMTool("llvm-objdump"), "-d", "--line-numbers", "--no-show-raw-insn", "--no-leading-addr", obj).Output()
	if err != nil {
		return "", nil, fmt.Errorf("llvm-objdump: %v", err)
	}
	var text []string
	var lines []int
	cur := 0
	started := false
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasSuffix(l, ">:") {
			started = true
			cur = 0
			if len(text) > 0 {
				text, lines = append(text, ""), append(lines, 0)
			}
			text, lines = append(text, strings.TrimSuffix(strings.TrimPrefix(strings.TrimLeft(l, "0123456789abcdef "), "<"), ">:")+":"), append(lines, 0)
			continue
		}
		if !started || strings.TrimSpace(l) == "" || strings.HasPrefix(l, "Disassembly of section") {
			continue
		}
		if m := reObjLine.FindStringSubmatch(l); m != nil {
			cur = 0
			if m[1] == main {
				cur, _ = strconv.Atoi(m[2])
			}
			continue
		}
		if strings.HasPrefix(l, "; ") {
			continue // "; main.sum():" and other function headings
		}
		text = append(text, "  "+strings.TrimSpace(strings.ReplaceAll(l, "\t", " ")))
		lines = append(lines, cur)
	}
	t, n := trimTrailing(text, lines)
	return t, n, nil
}

func trimTrailing(text []string, lines []int) (string, []int) {
	for len(text) > 0 && strings.TrimSpace(text[len(text)-1]) == "" {
		text, lines = text[:len(text)-1], lines[:len(lines)-1]
	}
	return strings.Join(text, "\n"), lines
}

// SizeReport is the binary's size by section and package.
type SizeReport struct {
	File     int64             `json:"file"`
	Text     uint64            `json:"text"`
	Sections []binsize.Section `json:"sections"`
	Packages []binsize.Section `json:"packages"`
}

func sizeReport(exe string) (*SizeReport, error) {
	b, err := binsize.Open(exe)
	if err != nil {
		return nil, err
	}
	r := &SizeReport{File: b.FileSize, Text: b.TextSize()}
	for _, s := range b.Sections {
		if s.Size > 0 && !strings.Contains(s.Name, "bss") && !strings.Contains(s.Name, "common") {
			r.Sections = append(r.Sections, s)
		}
	}
	sort.Slice(r.Sections, func(i, j int) bool { return r.Sections[i].Size > r.Sections[j].Size })
	if len(r.Sections) > 8 {
		r.Sections = r.Sections[:8]
	}
	pk := b.PackageSizes()
	for i := range pk {
		if pk[i].Name == "C" {
			pk[i].Name = "runtime core (C)"
		}
	}
	if len(pk) > 14 {
		var rest uint64
		for _, p := range pk[14:] {
			rest += p.Size
		}
		pk = append(pk[:14], binsize.Section{Name: fmt.Sprintf("%d more packages", len(b.PackageSizes())-14), Size: rest})
	}
	r.Packages = pk
	return r, nil
}
