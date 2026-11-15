package verify

import (
	"bufio"
	"bytes"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Gaurav-Gosain/marmot/internal/verify/smt"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// The soundness fuzzer: random functions that can panic, analyzed by
// engines A and B, then run under gc on many inputs. A panic at a line
// whose obligation (of the panic's kind) is reported proved is a
// soundness bug. MARMOT_SOUNDFUZZ=N runs N programs (default 12),
// MARMOT_SOUNDFUZZ_SEED picks the first seed; failing programs are kept
// in MARMOT_SOUNDFUZZ_KEEP (a directory) when set.

var reCase = regexp.MustCompile(`^F[0-9]+/[0-9]+$`)

type progGen struct {
	r     *rand.Rand
	b     strings.Builder
	depth int
	loops []string // loop variables in scope
	lines int
}

func (g *progGen) line(indent int, s string) {
	g.b.WriteString(strings.Repeat("\t", indent) + s + "\n")
	g.lines++
}

func (g *progGen) intAtom() string {
	atoms := []string{"a", "b", "x", "len(s)", "int(n)", "0", "1", "2", "3", "len(s) - 1", "len(s) + 1", "x & 7", "a - b", "b % 5"}
	if len(g.loops) > 0 && g.r.IntN(2) == 0 {
		return g.loops[g.r.IntN(len(g.loops))]
	}
	return atoms[g.r.IntN(len(atoms))]
}

func (g *progGen) idx() string {
	a := g.intAtom()
	if _, err := strconv.Atoi(a); err == nil {
		// A constant index, divisor or shift that is invalid does not
		// compile: make it a run-time value.
		a = "(x&0 + " + a + ")"
	}
	switch g.r.IntN(4) {
	case 0:
		return a + " + 1"
	case 1:
		return a + " - 1"
	}
	return a
}

func (g *progGen) cond() string {
	ops := []string{"<", "<=", ">", ">=", "==", "!="}
	return fmt.Sprintf("%s %s %s", g.intAtom(), ops[g.r.IntN(len(ops))], g.intAtom())
}

func (g *progGen) stmts(indent, n int) {
	for i := 0; i < n; i++ {
		g.stmt(indent)
	}
}

func (g *progGen) stmt(indent int) {
	k := g.r.IntN(12)
	if g.depth >= 3 && k >= 8 {
		k = g.r.IntN(8)
	}
	switch k {
	case 0, 1:
		g.line(indent, fmt.Sprintf("x += int(s[%s])", g.idx()))
	case 2:
		switch g.r.IntN(3) {
		case 0:
			g.line(indent, fmt.Sprintf("s = s[%s:]", g.idx()))
		case 1:
			g.line(indent, fmt.Sprintf("s = s[:%s]", g.idx()))
		default:
			g.line(indent, fmt.Sprintf("s = s[%s:%s]", g.idx(), g.idx()))
		}
	case 3:
		op := []string{"/", "%"}[g.r.IntN(2)]
		g.line(indent, fmt.Sprintf("x = x %s (%s)", op, g.idx()))
	case 4:
		g.line(indent, fmt.Sprintf("x = x << (%s)", g.idx()))
	case 5:
		g.line(indent, fmt.Sprintf("x = x*3 + %s", g.intAtom()))
	case 6:
		g.line(indent, fmt.Sprintf("if %s {", g.cond()))
		g.line(indent+1, "return x")
		g.line(indent, "}")
	case 7:
		g.line(indent, fmt.Sprintf("x -= %s", g.intAtom()))
	case 8, 9:
		g.depth++
		g.line(indent, fmt.Sprintf("if %s {", g.cond()))
		g.stmts(indent+1, 1+g.r.IntN(3))
		g.line(indent, "} else {")
		g.stmts(indent+1, 1+g.r.IntN(2))
		g.line(indent, "}")
		g.depth--
	default:
		g.depth++
		v := fmt.Sprintf("i%d", len(g.loops))
		lo := []string{"0", "1", "len(s) - 1", "b & 3"}[g.r.IntN(4)]
		hi := []string{"len(s)", "len(s) + 1", "min(a, 9)", "5", "len(s) - 1"}[g.r.IntN(5)]
		if g.r.IntN(3) == 0 {
			g.line(indent, fmt.Sprintf("for %s := %s; %s > %s; %s-- {", v, hi, v, lo, v))
		} else {
			g.line(indent, fmt.Sprintf("for %s := %s; %s < %s; %s++ {", v, lo, v, hi, v))
		}
		g.loops = append(g.loops, v)
		g.stmts(indent+1, 1+g.r.IntN(3))
		g.loops = g.loops[:len(g.loops)-1]
		g.line(indent, "}")
		g.depth--
	}
}

// genProgram writes nf functions F0..F{nf-1}(a, b int, s []byte, n uint8) int.
func genProgram(seed uint64, nf int) string {
	g := &progGen{r: rand.New(rand.NewPCG(seed, 99))}
	g.b.WriteString("package p\n")
	for f := 0; f < nf; f++ {
		fmt.Fprintf(&g.b, "\nfunc F%d(a, b int, s []byte, n uint8) int {\n\tx := a\n", f)
		g.stmts(1, 3+g.r.IntN(6))
		g.b.WriteString("\treturn x\n}\n")
	}
	return g.b.String()
}

const fuzzRunner = `package p

import (
	"fmt"
	"runtime"
	"testing"
)

func mk(n int, data string) []byte { b := make([]byte, len(data)); copy(b, data); return b[:n] }

func site(f func() int) (s string) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		pcs := make([]uintptr, 64)
		n := runtime.Callers(1, pcs)
		frames := runtime.CallersFrames(pcs[:n])
		past := false
		for {
			fr, more := frames.Next()
			if fr.Function == "runtime.gopanic" {
				past = true
			} else if past && len(fr.Function) > 8 && fr.Function[:8] != "runtime." {
				s = fmt.Sprintf("%d %q", fr.Line, fmt.Sprint(r))
				return
			}
			if !more {
				break
			}
		}
		s = fmt.Sprintf("0 %q", fmt.Sprint(r))
	}()
	f()
	return "ok"
}

func TestRun(t *testing.T) {
	for _, c := range cases {
		fmt.Println(c.name, site(c.f))
	}
}
`

func TestSoundFuzz(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs programs")
	}
	n, seed := 12, uint64(1)
	if v, err := strconv.Atoi(os.Getenv("MARMOT_SOUNDFUZZ")); err == nil {
		n = v
	}
	if v, err := strconv.ParseUint(os.Getenv("MARMOT_SOUNDFUZZ_SEED"), 10, 64); err == nil {
		seed = v
	}
	solver := smt.Z3()
	var proved, reached, bugs int
	for k := 0; k < n; k++ {
		s := seed + uint64(k)
		p, r, b := soundFuzzOne(t, s, solver)
		proved, reached, bugs = proved+p, reached+r, bugs+b
	}
	t.Logf("%d programs: %d obligations proved, %d panic sites reached, %d soundness bugs (solver %v)", n, proved, reached, bugs, solver != nil)
}

// soundFuzzOne checks one program; it returns the proved obligations,
// the distinct panic sites reached and the soundness bugs.
func soundFuzzOne(t *testing.T, seed uint64, solver *smt.Solver) (int, int, int) {
	const nf = 6
	src := genProgram(seed, nf)
	dir := t.TempDir()
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/p\n\ngo 1.22\n")
	write("p.go", src)
	pkgs, err := packages.Load(&packages.Config{Mode: packages.LoadAllSyntax, Dir: dir}, ".")
	if err != nil || packages.PrintErrors(pkgs) > 0 {
		t.Fatalf("seed %d: load: %v\n%s", seed, err, src)
	}
	prog, spkgs := ssautil.Packages(pkgs, 0)
	prog.Build()
	var funcs []*ssa.Function
	for fn := range ssautil.AllFunctions(prog) {
		if fn.Pkg == spkgs[0] && fn.Syntax() != nil {
			funcs = append(funcs, fn)
		}
	}
	opts := DefaultOptions
	opts.Solver = solver
	res := AnalyzePackage(prog.Fset, pkgs[0].Syntax, funcs, opts)
	// Proved obligations by function, line and kind.
	provedAt := map[string]bool{}
	nproved := 0
	for _, f := range res.Funcs {
		for _, o := range f.Obligations {
			if o.Status == Proved && o.Via == nil {
				nproved++
				provedAt[fmt.Sprintf("%s:%d:%s", f.Fn.Name(), prog.Fset.Position(o.Ob.Pos).Line, o.Ob.Kind)] = true
			}
		}
	}
	// Inputs.
	r := rand.New(rand.NewPCG(seed, 7))
	var cases strings.Builder
	cases.WriteString("package p\n\nvar cases = []struct {\n\tname string\n\tf    func() int\n}{\n")
	ints := []int64{0, 1, -1, 2, 3, 4, 5, 7, 8, 9, 16, -2, -9, 100, 1 << 40, -(1 << 40), 1<<63 - 1, -1 << 63}
	for f := 0; f < nf; f++ {
		for i := 0; i < 150; i++ {
			a, b := ints[r.IntN(len(ints))], ints[r.IntN(len(ints))]
			if r.IntN(3) == 0 {
				a, b = int64(r.IntN(20)-5), int64(r.IntN(20)-5)
			}
			ln := r.IntN(9)
			cp := ln + r.IntN(3)
			data := make([]byte, cp)
			for j := range data {
				data[j] = byte(r.IntN(256))
			}
			nn := r.IntN(256)
			fmt.Fprintf(&cases, "\t{%q, func() int { return F%d(%d, %d, mk(%d, %q), %d) }},\n", fmt.Sprintf("F%d/%d", f, i), f, a, b, ln, string(data), nn)
		}
	}
	cases.WriteString("}\n")
	write("cases_test.go", cases.String())
	write("run_test.go", fuzzRunner)
	cmd := exec.Command("go", "test", "-count=1", "-run", "TestRun", "-v", ".")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	if os.Getenv("MARMOT_SOUNDFUZZ_DEBUG") != "" {
		t.Logf("seed %d output:\n%s", seed, firstLines(string(out), 40))
	}
	if !bytes.Contains(out, []byte("\nPASS")) && !bytes.HasPrefix(out, []byte("PASS")) {
		t.Fatalf("seed %d: runner failed:\n%s\n%s", seed, firstLines(string(out), 20), src)
	}
	reached := map[string]bool{}
	bugs := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.SplitN(sc.Text(), " ", 3)
		if len(f) < 2 || !reCase.MatchString(f[0]) || f[1] == "ok" || len(f) != 3 {
			continue
		}
		fn, _, _ := strings.Cut(f[0], "/")
		msg, _ := strconv.Unquote(f[2])
		key := fmt.Sprintf("%s:%s:%s", fn, f[1], kindOfMessage(msg))
		if !reached[key] {
			reached[key] = true
			if provedAt[key] {
				bugs++
				t.Errorf("seed %d: %s panics (%s) but verify proved it", seed, key, msg)
				if keep := os.Getenv("MARMOT_SOUNDFUZZ_KEEP"); keep != "" {
					_ = os.MkdirAll(keep, 0o755)
					_ = os.WriteFile(filepath.Join(keep, fmt.Sprintf("seed%d.go", seed)), []byte(src), 0o644)
				}
			}
		}
	}
	return nproved, len(reached), bugs
}
