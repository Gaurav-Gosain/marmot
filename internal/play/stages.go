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
// the source line it comes from (a file of the request and a line): SSA
// from the instructions' positions, MLIR from loc() attributes, LLVM IR
// from !dbg locations (an inlined call's location is its call site in the
// request's files), machine code from the object's line table.

// srcLoc is a line of one of the request's files ("" when the text line
// comes from none of them).
type srcLoc struct {
	File string
	Line int
}

// fileNamer maps a path in the debug information to the name of the
// request's file it is, or "".
type fileNamer func(path string) string

// requestFiles names the .go files directly in dir.
func requestFiles(dir string) fileNamer {
	return func(p string) string {
		if filepath.Dir(p) == dir && strings.HasSuffix(p, ".go") {
			return filepath.Base(p)
		}
		return ""
	}
}

// ssaText is package main's SSA, function by function in source order.
func ssaText(root, dir string) (string, []srcLoc, error) {
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
	files := requestFiles(dir)
	lineOf := func(p token.Pos) srcLoc {
		if !p.IsValid() {
			return srcLoc{}
		}
		pos := fset.Position(p)
		if f := files(pos.Filename); f != "" {
			return srcLoc{f, pos.Line}
		}
		return srcLoc{}
	}
	var b strings.Builder
	var lines []srcLoc
	emit := func(s string, l srcLoc) {
		b.WriteString(s)
		b.WriteByte('\n')
		lines = append(lines, l)
	}
	for i, f := range uniq {
		if i > 0 {
			emit("", srcLoc{})
		}
		emit(fmt.Sprintf("func %s%s", f.RelString(pkg.Pkg), strings.TrimPrefix(f.Signature.String(), "func")), lineOf(f.Pos()))
		for _, bl := range f.Blocks {
			emit(fmt.Sprintf("%d:  %s", bl.Index, bl.Comment), srcLoc{})
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
// MLIR text and maps each operation to its source line.
func mlirLines(text string, files fileNamer) (string, []srcLoc) {
	defs := map[string]string{}
	for _, l := range strings.Split(text, "\n") {
		if m := reLocDef.FindStringSubmatch(l); m != nil {
			defs["#"+m[1]] = m[2]
		}
	}
	var resolve func(s string, depth int) srcLoc
	resolve = func(s string, depth int) srcLoc {
		if depth > 20 {
			return srcLoc{}
		}
		if m := reLocFile.FindStringSubmatch(s); m != nil {
			if f := files(m[1]); f != "" {
				n, _ := strconv.Atoi(m[2])
				return srcLoc{f, n}
			}
			return srcLoc{}
		}
		if strings.HasPrefix(s, "#loc") {
			return resolve(defs[s], depth+1)
		}
		// callsite(#callee at #caller), fused[...]: the first part in
		// the request's files, the caller last (an inlined callee from
		// std has no line of the request).
		for _, r := range reLocRef.FindAllString(s, -1) {
			if n := resolve(r, depth+1); n.Line > 0 {
				return n
			}
		}
		if m := reLocFile.FindStringSubmatch(strings.TrimLeft(s, "callsitefused[(")); m != nil {
			return resolve(m[0], depth+1)
		}
		return srcLoc{}
	}
	var out []string
	var lines []srcLoc
	var open []int // the lines that opened the regions not yet closed
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "#loc") {
			continue
		}
		var n srcLoc
		if m := reLocUse.FindAllStringSubmatch(l, -1); m != nil {
			n = resolve(m[len(m)-1][1], 0)
		}
		l = reLocUse.ReplaceAllString(l, "")
		// A region's location is printed after its closing brace (a
		// function's after the "}" of its body): give it to the line that
		// opens the region too.
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "}") && len(open) > 0 {
			o := open[len(open)-1]
			open = open[:len(open)-1]
			if lines[o].Line == 0 {
				lines[o] = n
			}
		}
		if strings.HasSuffix(t, "{") {
			open = append(open, len(out))
		}
		out = append(out, l)
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
// instruction with a location to its source line.
func llvmLines(text string, files fileNamer) (string, []srcLoc) {
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
	fileOf := func(scope string) string {
		for range 50 {
			n, ok := meta[scope]
			if !ok {
				return ""
			}
			if f, ok := n.fields["file"]; ok {
				fn := meta[f]
				name := unq(fn.fields["filename"])
				if !filepath.IsAbs(name) {
					name = filepath.Join(unq(fn.fields["directory"]), name)
				}
				return files(name)
			}
			scope = n.fields["scope"]
		}
		return ""
	}
	var lineOf func(id string, depth int) srcLoc
	lineOf = func(id string, depth int) srcLoc {
		n, ok := meta[id]
		if !ok || n.kind != "DILocation" || depth > 50 {
			return srcLoc{}
		}
		if f := fileOf(n.fields["scope"]); f != "" {
			v, _ := strconv.Atoi(n.fields["line"])
			return srcLoc{f, v}
		}
		if at, ok := n.fields["inlinedAt"]; ok {
			return lineOf(at, depth+1)
		}
		return srcLoc{}
	}
	var out []string
	var lines []srcLoc
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "!") || strings.HasPrefix(l, "attributes #") || strings.HasPrefix(l, "source_filename") || strings.HasPrefix(l, "; ModuleID") {
			continue
		}
		var n srcLoc
		if m := reDbgUse.FindStringSubmatch(l); m != nil {
			n = lineOf(m[1], 0)
		} else if strings.HasPrefix(l, "define ") {
			if m := regexp.MustCompile(`!dbg (!\d+)`).FindStringSubmatch(l); m != nil {
				if sp, ok := meta[m[1]]; ok {
					if f := fileOf(m[1]); f != "" {
						n.File = f
						n.Line, _ = strconv.Atoi(sp.fields["line"])
					}
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
func asmLines(obj string, files fileNamer) (string, []srcLoc, error) {
	out, err := exec.Command(driver.LLVMTool("llvm-objdump"), "-d", "--line-numbers", "--no-show-raw-insn", "--no-leading-addr", obj).Output()
	if err != nil {
		return "", nil, fmt.Errorf("llvm-objdump: %v", err)
	}
	var text []string
	var lines []srcLoc
	var cur srcLoc
	started := false
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasSuffix(l, ">:") {
			started = true
			cur = srcLoc{}
			if len(text) > 0 {
				text, lines = append(text, ""), append(lines, srcLoc{})
			}
			text, lines = append(text, strings.TrimSuffix(strings.TrimPrefix(strings.TrimLeft(l, "0123456789abcdef "), "<"), ">:")+":"), append(lines, srcLoc{})
			continue
		}
		if !started || strings.TrimSpace(l) == "" || strings.HasPrefix(l, "Disassembly of section") {
			continue
		}
		if m := reObjLine.FindStringSubmatch(l); m != nil {
			cur = srcLoc{}
			if f := files(m[1]); f != "" {
				cur.File = f
				cur.Line, _ = strconv.Atoi(m[2])
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

var (
	reInsn   = regexp.MustCompile(`^\s*([0-9a-f]+):\s+(.*)$`)
	reTarget = regexp.MustCompile(`\b0x([0-9a-f]+)\b`)
)

// asmFromExe disassembles package main's functions in a linked marmot
// executable, each instruction tied to its line by marmot's pc table:
// what tracebacks read, present in dev builds, whose objects carry no
// line table. Branch and call targets that start a function are named.
func asmFromExe(exe string, files fileNamer) (string, []srcLoc, error) {
	b, err := binsize.Open(exe)
	if err != nil {
		return "", nil, err
	}
	if b.Source != "pcln" {
		return "", nil, fmt.Errorf("%s has no pc table", filepath.Base(exe))
	}
	names := map[uint64]string{}
	for _, f := range b.Funcs {
		names[b.Base()+f.Start] = f.Name
	}
	var text []string
	var lines []srcLoc
	for _, f := range b.Funcs {
		if f.Pkg != "main" {
			continue
		}
		rows := b.Lines(f)
		lo, hi := b.Base()+f.Start, b.Base()+f.Start+f.Size
		out, err := exec.Command(driver.LLVMTool("llvm-objdump"), "-d", "--no-show-raw-insn",
			fmt.Sprintf("--start-address=0x%x", lo), fmt.Sprintf("--stop-address=0x%x", hi), exe).Output()
		if err != nil {
			return "", nil, fmt.Errorf("llvm-objdump: %v", err)
		}
		if len(text) > 0 {
			text, lines = append(text, ""), append(lines, srcLoc{})
		}
		text, lines = append(text, f.Name+":"), append(lines, srcLoc{})
		for _, l := range strings.Split(string(out), "\n") {
			m := reInsn.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			addr, err := strconv.ParseUint(m[1], 16, 64)
			if err != nil || addr < lo || addr >= hi {
				continue
			}
			var loc srcLoc
			off := addr - b.Base()
			for _, r := range rows {
				if r.Off > off {
					break
				}
				loc = srcLoc{}
				if name := files(r.File); name != "" {
					loc = srcLoc{name, r.Line}
				}
			}
			insn := strings.Join(strings.Fields(m[2]), " ")
			// "bl 0x100003f20 <ltmp0+0x20>": the function it starts.
			insn = regexp.MustCompile(` <[^>]*>$`).ReplaceAllString(insn, "")
			if t := reTarget.FindStringSubmatch(insn); t != nil {
				if v, err := strconv.ParseUint(t[1], 16, 64); err == nil && names[v] != "" {
					insn += " <" + names[v] + ">"
				}
			}
			text, lines = append(text, "  "+insn), append(lines, loc)
		}
	}
	t, n := trimTrailing(text, lines)
	return t, n, nil
}

func trimTrailing(text []string, lines []srcLoc) (string, []srcLoc) {
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
