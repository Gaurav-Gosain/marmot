package specialize

import (
	"fmt"
	"go/ast"
	"go/constant"
	"regexp/syntax"
	"strconv"
	"strings"
	"unicode/utf8"
)

// regexp specialization.
//
// regexp.MustCompile and regexp.Compile with a constant pattern get a
// matcher generated from the syntax.Prog that Compile builds for the
// pattern. The matcher runs the backtracking search of backtrack.go in the
// same priority order (start positions left to right, Alt Out before Arg,
// captures restored on backtrack), so its leftmost-first result is the one
// every general engine returns. It keeps no visited set, which changes only
// how much work a failing path costs: past a step budget it gives up and
// the general engine answers. The runtime checks that its own compiled
// program prints the same as the one the matcher was generated from.

// reMaxInsts bounds the program size the generator handles.
const reMaxInsts = 400

func rewriteRegexp(g *gen, f *ast.File) {
	if !g.on["regexp"] {
		return
	}
	rp := g.pkg.Imports["regexp"]
	if rp == nil || rp.Types == nil || rp.Types.Scope().Lookup("MarmotMustCompile") == nil {
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn := calleeFunc(g.info, call)
		if fn == nil || fn.Pkg().Path() != "regexp" || len(call.Args) != 1 || call.Ellipsis.IsValid() {
			return true
		}
		if fn.Name() != "MustCompile" && fn.Name() != "Compile" {
			return true
		}
		tv := g.info.Types[call.Args[0]]
		if tv.Value == nil || tv.Value.Kind() != constant.String {
			return true
		}
		expr := constant.StringVal(tv.Value)
		m, sig, ok := g.regexpMatcher(expr)
		if !ok {
			return true
		}
		wrap := m + "_compile"
		if fn.Name() == "Compile" {
			g.decls = append(g.decls, fmt.Sprintf("func %s(expr string) (*regexp.Regexp, error) {\n\treturn regexp.MarmotCompile(expr, %s, %s_prog)\n}\n", wrap, m, m))
		} else {
			g.decls = append(g.decls, fmt.Sprintf("func %s(expr string) *regexp.Regexp {\n\treturn regexp.MarmotMustCompile(expr, %s, %s_prog)\n}\n", wrap, m, m))
		}
		g.usePkg("regexp", "regexp")
		g.raw = append(g.raw, fmt.Sprintf("const %s_prog = %s\n", m, strconv.Quote(sig)))
		g.replaceCall(call, wrap, []ast.Expr{call.Args[0]})
		return true
	})
}

// regexpMatcher generates the matcher for expr and returns its name and
// the program text it was generated from.
func (g *gen) regexpMatcher(expr string) (string, string, bool) {
	re, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return "", "", false
	}
	prog, err := syntax.Compile(re.Simplify())
	if err != nil || len(prog.Inst) > reMaxInsts {
		return "", "", false
	}
	cond := prog.StartCond()
	if cond == ^syntax.EmptyOp(0) {
		return "", "", false
	}
	prefix, _ := prog.Prefix()
	nc := prog.NumCap
	if nc < 2 {
		nc = 2
	}
	name := g.newName("re")
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	var instVars []string
	w("func %s(s string, pos, ncap int, dstCap []int) ([]int, bool) {", name)
	if cond&syntax.EmptyBeginText != 0 {
		w("\tif pos != 0 {")
		w("\t\treturn nil, true")
		w("\t}")
	}
	w("\tend := len(s)")
	w("\tvar capa [%d]int", nc)
	w("\tfor i := range capa {")
	w("\t\tcapa[i] = -1")
	w("\t}")
	w("\tjp := regexp.MarmotGetJobs()")
	w("\tjobs := *jp")
	w("\tbudget := 4096 + %d*(end-pos+1)", 16*len(prog.Inst))
	w("\tsteps := 0")
	w("\tvar (")
	w("\t\tp, w int")
	w("\t\tr  rune")
	w("\t\tjb regexp.MarmotJob")
	w("\t)")
	w("\t_, _ = r, w")
	w("\tstart := pos")
	w("\twidth := -1")
	w("\tfor ; start <= end && width != 0; start += width {")
	if prefix != "" && cond&syntax.EmptyBeginText == 0 {
		w("\t\tif adv := strings.Index(s[start:], %s); adv < 0 {", strconv.Quote(prefix))
		w("\t\t\tregexp.MarmotPutJobs(jp, jobs)")
		w("\t\t\treturn nil, true")
		w("\t\t} else {")
		w("\t\t\tstart += adv")
		w("\t\t}")
		g.usePkg("strings", "strings")
	}
	w("\t\tif ncap > 0 {")
	w("\t\t\tcapa[0] = start")
	w("\t\t}")
	w("\t\tp = start")
	w("\t\tjobs = jobs[:0]")
	w("\t\tgoto L%d", prog.Start)
	// decode loads the rune at p into r and its width into w (0 at end).
	decode := func(ind string) {
		w(ind + "if p < end {")
		w(ind + "\tif c := s[p]; c < utf8.RuneSelf {")
		w(ind + "\t\tr, w = rune(c), 1")
		w(ind + "\t} else {")
		w(ind + "\t\tr, w = utf8.DecodeRuneInString(s[p:])")
		w(ind + "\t}")
		w(ind + "} else {")
		w(ind + "\tr, w = -1, 0")
		w(ind + "}")
	}
	var alts []int
	used := map[uint32]bool{uint32(prog.Start): true}
	for _, in := range prog.Inst {
		switch in.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			used[in.Out], used[in.Arg] = true, true
		case syntax.InstMatch, syntax.InstFail:
		default:
			used[in.Out] = true
		}
	}
	for pc, in := range prog.Inst {
		if !used[uint32(pc)] {
			continue
		}
		w("\tL%d:", pc)
		w("\t\tif steps++; steps > budget {")
		w("\t\t\tregexp.MarmotPutJobs(jp, jobs)")
		w("\t\t\treturn nil, false")
		w("\t\t}")
		switch in.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			alts = append(alts, pc)
			w("\t\tjobs = append(jobs, regexp.MarmotJob{PC: %d, Pos: p})", pc)
			w("\t\tgoto L%d", in.Out)
		case syntax.InstCapture:
			w("\t\tif %d < ncap {", in.Arg)
			w("\t\t\tjobs = append(jobs, regexp.MarmotJob{PC: -1 - %d, Pos: capa[%d]})", in.Arg, in.Arg)
			w("\t\t\tcapa[%d] = p", in.Arg)
			w("\t\t}")
			w("\t\tgoto L%d", in.Out)
		case syntax.InstEmptyWidth:
			w("\t\tif !regexp.MarmotEmpty(s, p, %d) {", in.Arg)
			w("\t\t\tgoto Lfail")
			w("\t\t}")
			w("\t\tgoto L%d", in.Out)
		case syntax.InstNop:
			w("\t\tgoto L%d", in.Out)
		case syntax.InstFail:
			w("\t\tgoto Lfail")
		case syntax.InstMatch:
			w("\t\tif ncap > 1 {")
			w("\t\t\tcapa[1] = p")
			w("\t\t}")
			w("\t\tregexp.MarmotPutJobs(jp, jobs)")
			w("\t\treturn append(dstCap, capa[:ncap]...), true")
		case syntax.InstRune1:
			r0 := in.Rune[0]
			if r0 < utf8.RuneSelf {
				w("\t\tif p < end && s[p] == %d {", r0)
				w("\t\t\tp++")
				w("\t\t\tgoto L%d", in.Out)
				w("\t\t}")
				w("\t\tgoto Lfail")
			} else {
				decode("\t\t")
				w("\t\tif r == %d {", r0)
				w("\t\t\tp += w")
				w("\t\t\tgoto L%d", in.Out)
				w("\t\t}")
				w("\t\tgoto Lfail")
			}
		case syntax.InstRuneAny, syntax.InstRuneAnyNotNL:
			decode("\t\t")
			if in.Op == syntax.InstRuneAny {
				w("\t\tif r == -1 {")
			} else {
				w("\t\tif r == -1 || r == '\\n' {")
			}
			w("\t\t\tgoto Lfail")
			w("\t\t}")
			w("\t\tp += w")
			w("\t\tgoto L%d", in.Out)
		case syntax.InstRune:
			decode("\t\t")
			cond := runeCond(in)
			if cond == "" {
				v := fmt.Sprintf("%s_i%d", name, pc)
				var rs []string
				for _, x := range in.Rune {
					rs = append(rs, strconv.Itoa(int(x)))
				}
				instVars = append(instVars, fmt.Sprintf("var %s = syntax.Inst{Op: syntax.InstRune, Arg: %d, Rune: []rune{%s}}\n", v, in.Arg, strings.Join(rs, ", ")))
				g.usePkg("regexp/syntax", "syntax")
				cond = v + ".MatchRune(r)"
			}
			w("\t\tif r != -1 && (%s) {", cond)
			w("\t\t\tp += w")
			w("\t\t\tgoto L%d", in.Out)
			w("\t\t}")
			w("\t\tgoto Lfail")
		default:
			return "", "", false
		}
	}
	w("\t\tgoto Lfail // keeps the label used")
	w("\tLfail:")
	w("\t\tif len(jobs) == 0 {")
	w("\t\t\tgoto Lnext")
	w("\t\t}")
	w("\t\tjb = jobs[len(jobs)-1]")
	w("\t\tjobs = jobs[:len(jobs)-1]")
	w("\t\tif jb.PC < 0 {")
	w("\t\t\tcapa[-1-jb.PC] = jb.Pos")
	w("\t\t\tgoto Lfail")
	w("\t\t}")
	w("\t\tp = jb.Pos")
	w("\t\tswitch jb.PC {")
	for _, pc := range alts {
		w("\t\tcase %d:", pc)
		w("\t\t\tgoto L%d", prog.Inst[pc].Arg)
	}
	w("\t\t}")
	w("\t\tgoto Lfail")
	w("\tLnext:")
	w("\t\tif start < end {")
	w("\t\t\tif c := s[start]; c < utf8.RuneSelf {")
	w("\t\t\t\twidth = 1")
	w("\t\t\t} else {")
	w("\t\t\t\t_, width = utf8.DecodeRuneInString(s[start:])")
	w("\t\t\t}")
	w("\t\t} else {")
	w("\t\t\twidth = 0")
	w("\t\t}")
	if cond&syntax.EmptyBeginText != 0 {
		w("\t\tbreak // anchored at the start of the text")
	}
	w("\t}")
	w("\tregexp.MarmotPutJobs(jp, jobs)")
	w("\treturn nil, true")
	w("}")
	g.usePkg("unicode/utf8", "utf8")
	g.decls = append(g.decls, strings.Join(instVars, "")+b.String())
	return name, prog.String(), true
}

// runeCond returns a condition on r for an InstRune without case folding,
// or "" to call Inst.MatchRune.
func runeCond(in syntax.Inst) string {
	if syntax.Flags(in.Arg)&syntax.FoldCase != 0 && len(in.Rune) == 1 {
		return ""
	}
	if len(in.Rune) == 1 {
		return fmt.Sprintf("r == %d", in.Rune[0])
	}
	if len(in.Rune)%2 != 0 || len(in.Rune) > 32 {
		return ""
	}
	var parts []string
	for i := 0; i < len(in.Rune); i += 2 {
		lo, hi := in.Rune[i], in.Rune[i+1]
		if lo == hi {
			parts = append(parts, fmt.Sprintf("r == %d", lo))
		} else {
			parts = append(parts, fmt.Sprintf("(r >= %d && r <= %d)", lo, hi))
		}
	}
	return strings.Join(parts, " || ")
}
