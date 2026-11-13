// Command fuzz is a differential fuzzer: it generates random valid integer and control flow
// programs, runs them with stock Go and with marmot, and reports any difference in stdout,
// stderr (up to the first panic line) or exit status. A failing program is shrunk by removing
// lines while the difference persists.
//
// Run from the repo root: go run ./tests/fuzz -n 200 -seed 1
//
// Generated programs never panic and never exceed constant ranges: divisors are forced odd,
// shift counts are reduced modulo a bound, loops have fixed small bounds.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	flN    = flag.Int("n", 100, "number of programs")
	flSeed = flag.Int64("seed", 1, "first seed; program i uses seed+i")
	flJ    = flag.Int("j", 4, "parallel jobs")
	flO    = flag.String("O", "", "marmot -O level")
	flKeep = flag.Bool("keep", false, "keep all generated programs under .work/fuzz")
	flShr  = flag.Bool("shrink", true, "shrink failing programs")
	flExt2 = flag.Bool("ext2", true, "also generate loops over slices, structs, fmt verbs, json round trips, sorts and struct-keyed maps")
	flMem  = flag.Bool("mem", true, "include append chains, retained slices and tree builders that hunt use-after-free")
	flStr  = flag.Bool("stress", false, "build and run with MARMOT_GCSTRESS=1")
	flRel  = flag.Bool("release", false, "build with marmot -release")
	flRoot = flag.String("root", "", "MARMOT_ROOT to use (a snapshot made by tests/snapshot.sh); default is the live tree")
	flGen  = flag.Int64("print", 0, "print the program for this seed and exit")
	flExt  = flag.Bool("ext", true, "also generate maps, strings, floats and goroutines")
)

type ivar struct {
	name string
	typ  string
	bits int
	sign bool
}

var types = []ivar{
	{"", "int8", 8, true}, {"", "int16", 16, true}, {"", "int32", 32, true}, {"", "int64", 64, true},
	{"", "uint8", 8, false}, {"", "uint16", 16, false}, {"", "uint32", 32, false}, {"", "uint64", 64, false},
	{"", "int", 64, true}, {"", "uint", 64, false},
}

type gen struct {
	jtypes []jtype
	decls  []string
	r      *rand.Rand
	vars   []ivar
	lines  []string
	ind    int
	depth  int
}

func (g *gen) emit(f string, a ...any) {
	g.lines = append(g.lines, strings.Repeat("\t", g.ind)+fmt.Sprintf(f, a...))
}

// lit returns a literal that fits type t.
func (g *gen) lit(t ivar) string {
	switch g.r.Intn(4) {
	case 0:
		return fmt.Sprint(g.r.Intn(10))
	case 1:
		if t.sign {
			return fmt.Sprint(-g.r.Intn(100))
		}
		return fmt.Sprint(g.r.Intn(100))
	}
	u := g.r.Uint64()
	if t.sign {
		return fmt.Sprint(int64(u) >> uint(64-t.bits))
	}
	return fmt.Sprint(u >> uint(64-t.bits))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (g *gen) ofType(t ivar) ivar {
	var c []ivar
	for _, v := range g.vars {
		if v.typ == t.typ {
			c = append(c, v)
		}
	}
	if len(c) == 0 {
		return ivar{}
	}
	return c[g.r.Intn(len(c))]
}

// expr returns an expression of type t.
func (g *gen) expr(t ivar, d int) string {
	if d <= 0 || g.r.Intn(4) == 0 {
		if v := g.ofType(t); v.name != "" && g.r.Intn(3) != 0 {
			return v.name
		}
		if g.r.Intn(3) == 0 {
			// Conversion from another variable.
			o := g.vars[g.r.Intn(len(g.vars))]
			return fmt.Sprintf("%s(%s)", t.typ, o.name)
		}
		return fmt.Sprintf("(z_%s + %s)", t.typ, g.lit(t))
	}
	a, b := g.expr(t, d-1), g.expr(t, d-1)
	switch g.r.Intn(12) {
	case 0:
		return fmt.Sprintf("(%s + %s)", a, b)
	case 1:
		return fmt.Sprintf("(%s - %s)", a, b)
	case 2:
		return fmt.Sprintf("(%s * %s)", a, b)
	case 3:
		return fmt.Sprintf("(%s / (%s | 1))", a, b)
	case 4:
		return fmt.Sprintf("(%s %% (%s | 1))", a, b)
	case 5:
		return fmt.Sprintf("(%s & %s)", a, b)
	case 6:
		return fmt.Sprintf("(%s | %s)", a, b)
	case 7:
		return fmt.Sprintf("(%s ^ %s)", a, b)
	case 8:
		return fmt.Sprintf("(%s &^ %s)", a, b)
	case 9:
		return fmt.Sprintf("(%s << (uint(%s) %% %d))", a, g.anyVar(), t.bits+3)
	case 10:
		return fmt.Sprintf("(%s >> (uint(%s) %% %d))", a, g.anyVar(), t.bits+3)
	}
	return fmt.Sprintf("(-%s)", a)
}

func (g *gen) anyVar() string { return g.vars[g.r.Intn(len(g.vars))].name }

func (g *gen) cond() string {
	t := g.vars[g.r.Intn(len(g.vars))]
	ops := []string{"<", "<=", ">", ">=", "==", "!="}
	c := fmt.Sprintf("%s %s %s", g.expr(t, 1), ops[g.r.Intn(len(ops))], g.expr(t, 1))
	if g.r.Intn(4) == 0 {
		t2 := g.vars[g.r.Intn(len(g.vars))]
		c += fmt.Sprintf(" && %s != %s", t2.name, g.expr(t2, 1))
	}
	return c
}

// extStmt emits a statement over the map m, string s, float f or a goroutine fan-out.
func (g *gen) extStmt() {
	v := g.vars[g.r.Intn(len(g.vars))]
	for v.name[0] != 'v' {
		v = g.vars[g.r.Intn(len(g.vars))]
	}
	switch g.r.Intn(9) {
	case 0:
		g.emit("m[int(%s)%%8] += int(%s)", g.expr(v, 2), g.anyVar())
	case 1:
		g.emit("delete(m, int(%s)%%8)", g.anyVar())
	case 2:
		g.emit("s += string(rune('a' + uint(%s)%%26))", g.expr(v, 2))
	case 3:
		g.emit("if len(s) > 3 {")
		g.emit("\ts = s[len(s)/3:]")
		g.emit("}")
	case 4:
		g.emit("f = float64(f*1.25) + float64(%s)", g.expr(v, 2))
	case 5:
		g.emit("f = float64(f/3) - float64(%s)", g.anyVar())
	case 6:
		g.emit("%s += %s(len(s)) + %s(len(m))", v.name, v.typ, v.typ)
	case 7:
		g.emit("for k := 0; k < 8; k++ {")
		g.emit("\t%s ^= %s(m[k]) + %s(k)", v.name, v.typ, v.typ)
		g.emit("}")
	default:
		k := 2 + g.r.Intn(4)
		g.emit("{")
		g.emit("\tres := make([]%s, %d)", v.typ, k)
		g.emit("\tdone := make(chan bool)")
		g.emit("\tfor w := 0; w < %d; w++ {", k)
		g.emit("\t\tgo func() {")
		g.emit("\t\t\tres[w] = %s + %s(w)", g.expr(v, 2), v.typ)
		g.emit("\t\t\tdone <- true")
		g.emit("\t\t}()")
		g.emit("\t}")
		g.emit("\tfor w := 0; w < %d; w++ {", k)
		g.emit("\t\t<-done")
		g.emit("\t}")
		g.emit("\tfor w := range res {")
		g.emit("\t\t%s += res[w]", v.name)
		g.emit("\t}")
		g.emit("}")
	}
}

func (g *gen) stmt() {
	if *flExt2 && g.depth == 0 && g.r.Intn(3) == 0 {
		g.ext2Stmt()
		return
	}
	if *flExt && g.r.Intn(4) == 0 {
		g.extStmt()
		return
	}
	switch k := g.r.Intn(10); {
	case k < 5 || g.depth >= 3:
		v := g.vars[g.r.Intn(len(g.vars))]
		for v.name[0] != 'v' { // loop counters are read only, otherwise a loop may never end
			v = g.vars[g.r.Intn(len(g.vars))]
		}
		ops := []string{"=", "+=", "-=", "*=", "^=", "|=", "&="}
		g.emit("%s %s %s", v.name, ops[g.r.Intn(len(ops))], g.expr(v, 3))
	case k < 7:
		g.emit("if %s {", g.cond())
		g.block(1 + g.r.Intn(3))
		if g.r.Intn(2) == 0 {
			g.emit("} else {")
			g.block(1 + g.r.Intn(3))
		}
		g.emit("}")
	case k < 9:
		i := fmt.Sprintf("i%d", len(g.lines))
		g.emit("for %s := 0; %s < %d; %s++ {", i, i, 1+g.r.Intn(8), i)
		g.vars = append(g.vars, ivar{i, "int", 64, true})
		g.block(1 + g.r.Intn(3))
		g.vars = g.vars[:len(g.vars)-1]
		g.emit("}")
	default:
		v := g.vars[g.r.Intn(len(g.vars))]
		g.emit("switch %s %% 4 {", g.expr(v, 1))
		for c := 0; c < 2; c++ {
			g.emit("case %d:", c)
			g.block(1)
		}
		g.emit("default:")
		g.block(1)
		g.emit("}")
	}
}

func (g *gen) block(n int) {
	g.ind++
	g.depth++
	for i := 0; i < n; i++ {
		g.stmt()
	}
	g.depth--
	g.ind--
}

func program(seed int64) string {
	g := &gen{r: rand.New(rand.NewSource(seed))}
	if *flExt2 {
		g.genDecls()
		g.emit("package main")
		g.emit("")
		g.emit("import (")
		for _, im := range []string{"cmp", "encoding/json", "fmt", "slices", "sort", "strings"} {
			g.emit("\t%q", im)
		}
		g.emit(")")
		g.emit("")
		g.emit("var _ = cmp.Compare[int]")
		g.emit("var _ = json.Marshal")
		g.emit("var _ = fmt.Sprint")
		g.emit("var _ = slices.Sort[[]int]")
		g.emit("var _ = sort.Ints")
		g.emit("var _ strings.Builder")
		g.emit("")
		for _, d := range g.decls {
			g.lines = append(g.lines, strings.TrimRight(d, "\n"))
		}
		g.emit("")
	} else {
		g.emit("package main")
		g.emit("")
	}
	for _, t := range types {
		g.emit("var z_%s %s", t.typ, t.typ)
	}
	g.emit("")
	g.emit("func main() {")
	g.ind = 1
	nv := 4 + g.r.Intn(5)
	for i := 0; i < nv; i++ {
		t := types[g.r.Intn(len(types))]
		t.name = fmt.Sprintf("v%d", i)
		g.emit("var %s %s = %s", t.name, t.typ, g.lit(t))
		g.emit("_ = %s", t.name)
		g.vars = append(g.vars, t)
	}
	if *flExt {
		g.emit("m := map[int]int{}")
		g.emit("_ = m")
		g.emit(`s := ""`)
		g.emit("_ = s")
		g.emit("f := 1.0")
		g.emit("_ = f")
	}
	n := 8 + g.r.Intn(12)
	for i := 0; i < n; i++ {
		g.stmt()
		if g.r.Intn(5) == 0 {
			g.emit("println(%s)", g.anyVar())
		}
	}
	for _, v := range g.vars {
		g.emit("println(%s)", v.name)
	}
	if *flExt {
		g.emit("for k := 0; k < 8; k++ {")
		g.emit("\tprintln(k, m[k])")
		g.emit("}")
		g.emit("println(len(m), s, len(s), f)")
	}
	g.ind = 0
	g.emit("}")
	return strings.Join(g.lines, "\n") + "\n"
}

type outcome struct {
	stdout, stderr string
	code           int
	ok             bool // built and ran to an exit
}

func sh(timeout time.Duration, dir string, env []string, name string, args ...string) (outcome, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.WaitDelay = time.Second
	killGroupOnCancel(cmd)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	o := outcome{stdout: so.String(), stderr: so.String() + se.String()}
	o.stderr = se.String()
	if ctx.Err() != nil {
		return o, "timeout"
	}
	if ee, ok := err.(*exec.ExitError); ok {
		o.code = ee.ExitCode()
	} else if err != nil {
		return o, err.Error()
	}
	o.ok = true
	return o, ""
}

type runner struct {
	root, mroot, work, marmot string
	mu                        sync.Mutex
	n                         int
}

// check compiles and runs src with both toolchains. It returns a description of the difference,
// or "" when they agree or when stock Go rejects the program.
func (rn *runner) check(src string) string {
	rn.mu.Lock()
	rn.n++
	id := rn.n
	rn.mu.Unlock()
	dir := filepath.Join(rn.work, fmt.Sprint("t", id))
	os.MkdirAll(dir, 0o755)
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "p.go")
	os.WriteFile(file, []byte(src), 0o644)
	ref, why := sh(2*time.Minute, rn.root, nil, "go", "build", "-o", filepath.Join(dir, "ref"), file)
	if why != "" || ref.code != 0 {
		return ""
	}
	want, why := sh(10*time.Second, dir, nil, filepath.Join(dir, "ref"))
	if why != "" {
		return ""
	}
	args := []string{"build", "-o", filepath.Join(dir, "got")}
	if *flRel {
		args = append(args, "-release")
	}
	if *flO != "" {
		args = append(args, "-O", *flO)
	}
	args = append(args, file)
	b, why := sh(2*time.Minute, rn.root, append(append([]string{"MARMOT_ROOT=" + rn.mroot, "MARMOT_CACHE_MAX_MB=512"}, tier1Off()...), stressEnv()...), rn.marmot, args...)
	if why != "" || b.code != 0 {
		return "compile error: " + firstLine(b.stdout+b.stderr+why)
	}
	got, why := sh(10*time.Second, dir, stressEnv(), filepath.Join(dir, "got"))
	if why != "" {
		return "run: " + why
	}
	switch {
	case got.code != want.code:
		return fmt.Sprintf("exit %d, want %d", got.code, want.code)
	case got.stdout != want.stdout:
		return "stdout differs"
	case got.stderr != want.stderr:
		return "stderr differs: " + diffLine(want.stderr, got.stderr)
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func diffLine(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return fmt.Sprintf("line %d want %q got %q", i+1, a, b)
		}
	}
	return ""
}

// shrink removes lines while the program still fails the same way (any difference).
func (rn *runner) shrink(src string) string {
	lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
	for changed := true; changed; {
		changed = false
		for i := len(lines) - 1; i >= 0; i-- {
			t := strings.TrimSpace(lines[i])
			if t == "package main" || t == "func main() {" || t == "" {
				continue
			}
			cand := append(append([]string{}, lines[:i]...), lines[i+1:]...)
			if rn.check(strings.Join(cand, "\n")+"\n") != "" {
				lines = cand
				changed = true
			}
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func main() {
	// Plain compiler output whoever runs the harness (marmot switches to
	// JSON under coding agents).
	os.Setenv("MARMOT_OUTPUT", "plain")
	flag.Parse()
	if *flGen != 0 {
		fmt.Print(program(*flGen))
		return
	}
	root, _ := os.Getwd()
	if os.Getenv("GOCACHE") == "" {
		os.Setenv("GOCACHE", filepath.Join(root, ".work", "gocache"))
	}
	work := filepath.Join(root, ".work", "fuzz")
	os.MkdirAll(work, 0o755)
	mroot := root
	if *flRoot != "" {
		mroot = *flRoot
	}
	marmot := filepath.Join(mroot, "build", "bin", "marmot")
	if _, err := os.Stat(marmot); err != nil {
		fmt.Fprintln(os.Stderr, "fuzz: build/bin/marmot not found (make frontend)")
		os.Exit(2)
	}
	rn := &runner{root: root, mroot: mroot, work: work, marmot: marmot}
	var wg sync.WaitGroup
	sem := make(chan struct{}, min(max(*flJ, 1), 4))
	var mu sync.Mutex
	fails := 0
	for i := 0; i < *flN; i++ {
		seed := *flSeed + int64(i)
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			src := program(seed)
			if *flKeep {
				os.WriteFile(filepath.Join(work, fmt.Sprintf("seed%d.go", seed)), []byte(src), 0o644)
			}
			why := rn.check(src)
			if why == "" {
				return
			}
			if *flShr {
				src = rn.shrink(src)
			}
			path := filepath.Join(work, fmt.Sprintf("fail_seed%d.go", seed))
			os.WriteFile(path, []byte(src), 0o644)
			mu.Lock()
			fails++
			fmt.Printf("FAIL seed %d: %s\n  program: %s (%d lines)\n", seed, why, path, strings.Count(src, "\n"))
			mu.Unlock()
		}()
	}
	wg.Wait()
	fmt.Printf("%d programs, %d differences\n", *flN, fails)
	if fails > 0 {
		os.Exit(1)
	}
}

// killGroupOnCancel runs cmd in its own process group and kills the whole group when the
// context ends, so children of a hung compile or test never outlive it.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

func stressEnv() []string {
	if *flStr {
		return []string{"MARMOT_GCSTRESS=1"}
	}
	return nil
}

// tier1Off keeps background tier 1 out of the runs (it would compete for
// cores with the builds being measured) unless MARMOT_TIER1 is set.
func tier1Off() []string {
	if os.Getenv("MARMOT_TIER1") != "" {
		return nil
	}
	return []string{"MARMOT_TIER1=0"}
}
