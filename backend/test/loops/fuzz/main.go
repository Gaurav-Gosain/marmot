// Command fuzz is a differential fuzzer for the loop optimizer.
// It generates Go programs made of loop shapes the passes target (reductions,
// strided and offset stores, overlapping sub-slices, bounds checks that may
// fail, scans, histograms, nested loops, escape loops), builds each with stock
// Go and with marmot with every loop pass forced on (-par -type-alias
// -loops-force-par), and reports any difference in output or exit status.
//
//	go run ./backend/test/loops/fuzz -n 100 -seed 1 -j 2
//
// Failing programs are kept in .work/loops/fuzz/fail-SEED.go.
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
	"time"
)

var (
	flN     = flag.Int("n", 50, "number of programs")
	flSeed  = flag.Int64("seed", 1, "first seed")
	flJ     = flag.Int("j", 2, "parallel jobs")
	flFlags = flag.String("flags", "-par -type-alias -loops-force-par -loops-all-rules", "MARMOT_BACKEND_FLAGS for marmot")
	flKeep  = flag.Bool("keep", false, "keep every program")
	flPrint = flag.Int64("print", 0, "print the program for this seed and exit")
)

type gen struct {
	r   *rand.Rand
	b   strings.Builder
	fns []string
	n   int
}

func (g *gen) pick(xs ...string) string { return xs[g.r.Intn(len(xs))] }

func (g *gen) intType() string { return g.pick("int64", "int32", "uint32", "int16", "uint8", "int") }

// An index expression over i in [0, n) that stays inside [0, n+pad).
func (g *gen) index(pad int) string {
	switch g.r.Intn(6) {
	case 0:
		return "i"
	case 1:
		return fmt.Sprintf("i+%d", g.r.Intn(pad+1))
	case 2:
		return "n-1-i"
	case 3:
		return "(i*7)%n"
	case 4:
		return fmt.Sprintf("i/%d", 1+g.r.Intn(3))
	default:
		return "i"
	}
}

func (g *gen) expr(t, v string) string {
	switch g.r.Intn(7) {
	case 0:
		return fmt.Sprintf("%s(%s*3+1)", t, v)
	case 1:
		return fmt.Sprintf("%s(%s)^%s(i)", t, v, t)
	case 2:
		return fmt.Sprintf("%s(%s>>1)+%s(%d)", t, v, t, g.r.Intn(9))
	case 3:
		return fmt.Sprintf("%s(%s)*%s(i&15)", t, v, t)
	case 4:
		return fmt.Sprintf("%s(%s-%d)", t, v, g.r.Intn(100))
	default:
		return fmt.Sprintf("%s(%s)+%s(i)", t, v, t)
	}
}

func (g *gen) fn() {
	g.n++
	name := fmt.Sprintf("f%d", g.n)
	t := g.intType()
	b := &g.b
	switch g.r.Intn(14) {
	case 12: // append, with or without enough capacity, into a shared array
		extra := g.r.Intn(3) * 1000
		fmt.Fprintf(b, "//go:noinline\nfunc %s(s []%s, n int) []%s {\n\tfor i := 0; i < n; i++ {\n\t\ts = append(s, %s)\n\t}\n\treturn s\n}\n\n",
			name, t, t, g.expr(t, "i"))
		g.fns = append(g.fns, fmt.Sprintf("{ base := make([]%s, 5, N/2+%d); sh := base[:cap(base)]; r := %s(base, N/2); print(\"%s\", len(r), sum%s(r), sum%s(sh)) }",
			t, extra, name, name, t, t))
		return
	case 13: // narrow polynomial recurrence over many elements
		nt := g.pick("int8", "int16", "uint8", "uint16", "int32")
		fmt.Fprintf(b, "//go:noinline\nfunc %s(buf []%s) {\n\tvar d, a %s\n\tfor i := range buf {\n\t\td += %d\n\t\ta += d\n\t\tbuf[i] = a\n\t}\n}\n\n",
			name, nt, nt, 1+g.r.Intn(5))
		g.fns = append(g.fns, fmt.Sprintf("{ pb := make([]%s, 70000+N); %s(pb); s := int64(0); for _, v := range pb { s = s*31 + int64(v) }; println(\"%s\", s, pb[65535], pb[65536+N%%7]) }",
			nt, name, name))
		return
	case 0: // reduction
		op := g.pick("+=", "^=", "|=", "&=", "-=")
		fmt.Fprintf(b, "//go:noinline\nfunc %s(a []%s) %s {\n\tvar s %s = %d\n\tfor i := range a {\n\t\ts %s %s\n\t}\n\treturn s\n}\n\n",
			name, t, t, t, g.r.Intn(5), op, g.expr(t, "a[i]"))
		g.fns = append(g.fns, fmt.Sprintf("print(\"%s\", %s(A%s[:N]))", name, name, t))
	case 1: // min/max
		cmp := g.pick("<", ">")
		fmt.Fprintf(b, "//go:noinline\nfunc %s(a []%s) %s {\n\tm := a[0]\n\tfor _, v := range a {\n\t\tif v %s m {\n\t\t\tm = v\n\t\t}\n\t}\n\treturn m\n}\n\n", name, t, t, cmp)
		g.fns = append(g.fns, fmt.Sprintf("print(\"%s\", %s(A%s[:N]))", name, name, t))
	case 2: // map into another slice, index shapes
		ix := g.index(4)
		fmt.Fprintf(b, "//go:noinline\nfunc %s(d, a []%s, n int) {\n\tfor i := 0; i < n; i++ {\n\t\td[i] = %s\n\t}\n}\n\n", name, t, g.expr(t, "a["+ix+"]"))
		g.fns = append(g.fns, fmt.Sprintf("%s(D%s, A%s, N); print(\"%s\", sum%s(D%s[:N]))", name, t, t, name, t, t))
	case 3: // in place with offsets (may overlap across iterations)
		off := g.r.Intn(3)
		fmt.Fprintf(b, "//go:noinline\nfunc %s(a []%s, n int) {\n\tfor i := 0; i < n; i++ {\n\t\ta[i+%d] = %s\n\t}\n}\n\n", name, t, off, g.expr(t, "a[i]"))
		g.fns = append(g.fns, fmt.Sprintf("copy(D%s, A%s); %s(D%s, N); print(\"%s\", sum%s(D%s[:N+4]))", t, t, name, t, name, t, t))
	case 4: // overlapping sub-slices passed as two arguments
		shift := g.r.Intn(5) - 2
		fmt.Fprintf(b, "//go:noinline\nfunc %s(d, a []%s) {\n\tfor i := range d {\n\t\td[i] = %s\n\t}\n}\n\n", name, t, g.expr(t, "a[i]"))
		g.fns = append(g.fns, fmt.Sprintf("copy(D%s, A%s); %s(D%s[%d:N+%d], D%s[%d:N+%d]); print(\"%s\", sum%s(D%s[:N+4]))",
			t, t, name, t, 2+shift, 2+shift, t, 2, 2, name, t, t))
	case 5: // bounds check that may fail: n may exceed len
		extra := g.r.Intn(2) * g.r.Intn(3)
		fmt.Fprintf(b, "//go:noinline\nfunc %s(d []%s, n int) (msg string) {\n\tdefer func() {\n\t\tif r := recover(); r != nil {\n\t\t\tmsg = r.(error).Error()\n\t\t}\n\t}()\n\tfor i := 0; i < n; i++ {\n\t\td[i] = %s\n\t}\n\treturn \"ok\"\n}\n\n", name, t, g.expr(t, "i"))
		g.fns = append(g.fns, fmt.Sprintf("print(\"%s\", %s(D%s[:N], N+%d)); print(\"%s\", sum%s(D%s[:N]))", name, name, t, extra, name, t, t))
	case 6: // scan
		op := g.pick("+=", "^=", "|=")
		fmt.Fprintf(b, "//go:noinline\nfunc %s(a []%s) {\n\tfor i := 1; i < len(a); i++ {\n\t\ta[i] %s a[i-1]\n\t}\n}\n\n", name, t, op)
		g.fns = append(g.fns, fmt.Sprintf("copy(D%s, A%s); %s(D%s[:N]); print(\"%s\", D%s[N-1], D%s[N/2], sum%s(D%s[:N]))", t, t, name, t, name, t, t, t, t))
	case 7: // histogram
		fmt.Fprintf(b, "//go:noinline\nfunc %s(a []%s) [64]int {\n\tvar h [64]int\n\tfor _, v := range a {\n\t\th[uint64(v)&63]++\n\t}\n\treturn h\n}\n\n", name, t)
		g.fns = append(g.fns, fmt.Sprintf("{ h := %s(A%s[:N]); s := 0; for k, c := range h { s += k * c }; print(\"%s\", s) }", name, t, name))
	case 8: // nested rows
		fmt.Fprintf(b, "//go:noinline\nfunc %s(m [][]%s) %s {\n\tvar s %s\n\tfor i := range m {\n\t\tfor j := range m[i] {\n\t\t\tm[i][j] = %s\n\t\t\ts += m[i][j]\n\t\t}\n\t}\n\treturn s\n}\n\n", name, t, t, t, g.expr(t, "j*3+i"))
		g.fns = append(g.fns, fmt.Sprintf("print(\"%s\", %s(M%s))", name, name, t))
	case 9: // count
		fmt.Fprintf(b, "//go:noinline\nfunc %s(a []uint8, c uint8) int {\n\tn := 0\n\tfor _, x := range a {\n\t\tif x %s c {\n\t\t\tn++\n\t\t}\n\t}\n\treturn n\n}\n\n", name, g.pick("==", "!="))
		g.fns = append(g.fns, fmt.Sprintf("print(\"%s\", %s(Auint8[:N], %d))", name, name, g.r.Intn(256)))
	case 10: // escape loop
		fmt.Fprintf(b, "//go:noinline\nfunc %s(out []int32, n int) int {\n\tc := 0\n\tfor p := 0; p < n; p++ {\n\t\tz := float64(p%%97) / 40\n\t\tk := 0\n\t\tfor ; k < %d && z < 3; k++ {\n\t\t\tz = z*z*0.9 + 0.31\n\t\t}\n\t\tout[p] = int32(k)\n\t\tc += k\n\t}\n\treturn c\n}\n\n", name, 5+g.r.Intn(40))
		g.fns = append(g.fns, fmt.Sprintf("{ o := make([]int32, N); print(\"%s\", %s(o, N), o[N/3]) }", name, name))
	default: // strided column writes
		fmt.Fprintf(b, "//go:noinline\nfunc %s(b, a []%s, w, h int) {\n\tfor i := 0; i < w; i++ {\n\t\tfor j := 0; j < h; j++ {\n\t\t\tb[j*w+i] = %s\n\t\t}\n\t}\n}\n\n", name, t, g.expr(t, "a[i*h+j]"))
		g.fns = append(g.fns, fmt.Sprintf("%s(D%s, A%s, 37, N/37); print(\"%s\", sum%s(D%s[:N]))", name, t, t, name, t, t))
	}
}

var types = []string{"int64", "int32", "uint32", "int16", "uint8", "int"}

func program(seed int64) string {
	g := &gen{r: rand.New(rand.NewSource(seed))}
	n := 1000 + g.r.Intn(70000)
	fmt.Fprintf(&g.b, "// loops fuzz program, seed %d\npackage main\n\nconst N = %d\n\n", seed, n)
	for _, t := range types {
		fmt.Fprintf(&g.b, "var A%s, D%s []%s\nvar M%s [][]%s\n\n", t, t, t, t, t)
		fmt.Fprintf(&g.b, "func sum%s(a []%s) %s {\n\tvar s %s\n\tfor _, v := range a {\n\t\ts = s*31 + v\n\t}\n\treturn s\n}\n\n", t, t, t, t)
	}
	k := 3 + g.r.Intn(6)
	for i := 0; i < k; i++ {
		g.fn()
	}
	g.b.WriteString("func print(name string, vs ...any) {\n\tprintln(name)\n\tfor _, v := range vs {\n\t\tswitch x := v.(type) {\n")
	for _, t := range types {
		fmt.Fprintf(&g.b, "\t\tcase %s:\n\t\t\tprintln(int64(x))\n", t)
	}
	g.b.WriteString("\t\tcase string:\n\t\t\tprintln(x)\n\t\t}\n\t}\n}\n\nfunc main() {\n")
	for _, t := range types {
		fmt.Fprintf(&g.b, "\tA%s = make([]%s, N+8)\n\tD%s = make([]%s, N+8)\n\tfor i := range A%s {\n\t\tA%s[i] = %s(i*2654435761>>7) - %s(i&255)\n\t}\n", t, t, t, t, t, t, t, t)
		fmt.Fprintf(&g.b, "\tM%s = make([][]%s, 9)\n\tfor i := range M%s {\n\t\tM%s[i] = make([]%s, N/9+i)\n\t}\n", t, t, t, t, t)
	}
	for _, c := range g.fns {
		g.b.WriteString("\t" + c + "\n")
	}
	g.b.WriteString("}\n")
	return g.b.String()
}

func run(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func status(err error) string {
	if err == nil {
		return "exit 0"
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return fmt.Sprintf("exit %d", ee.ExitCode())
	}
	return err.Error()
}

func main() {
	flag.Parse()
	if *flPrint != 0 {
		fmt.Print(program(*flPrint))
		return
	}
	root, _ := os.Getwd()
	work := filepath.Join(root, ".work", "loops", "fuzz")
	os.MkdirAll(work, 0o755)
	marmot := filepath.Join(root, "build", "bin", "marmot")
	var mu sync.Mutex
	fails := 0
	sem := make(chan struct{}, *flJ)
	var wg sync.WaitGroup
	for i := 0; i < *flN; i++ {
		seed := *flSeed + int64(i)
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			dir := filepath.Join(work, fmt.Sprintf("s%d", seed))
			os.MkdirAll(dir, 0o755)
			src := filepath.Join(dir, "main.go")
			os.WriteFile(src, []byte(program(seed)), 0o644)
			os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fz\n\ngo 1.22\n"), 0o644)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			res := "ok"
			if out, err := run(ctx, nil, "go", "build", "-C", dir, "-o", "go.bin", "."); err != nil {
				res = "go build failed: " + out
			} else if out, err := run(ctx, []string{"MARMOT_ROOT=" + root, "MARMOT_BACKEND_FLAGS=" + *flFlags},
				marmot, "build", "-nocache", "-o", filepath.Join(dir, "m.bin"), src); err != nil {
				res = "marmot build failed: " + out
			} else {
				g, gerr := run(ctx, nil, filepath.Join(dir, "go.bin"))
				for _, procs := range []string{"1", "8"} {
					m, merr := run(ctx, []string{"GOMAXPROCS=" + procs}, filepath.Join(dir, "m.bin"))
					if m != g || status(gerr) != status(merr) {
						res = fmt.Sprintf("DIFF (GOMAXPROCS=%s)\n--- go (%s)\n%s\n--- marmot (%s)\n%s", procs, status(gerr), g, status(merr), m)
						break
					}
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if res != "ok" {
				fails++
				os.WriteFile(filepath.Join(work, fmt.Sprintf("fail-%d.go", seed)), []byte(program(seed)), 0o644)
				fmt.Printf("seed %d: %s\n", seed, firstLines(res, 12))
			}
			if res == "ok" && !*flKeep {
				os.RemoveAll(dir)
			}
		}()
	}
	wg.Wait()
	fmt.Printf("%d programs, %d failures\n", *flN, fails)
	if fails > 0 {
		os.Exit(1)
	}
}

func firstLines(s string, n int) string {
	ls := strings.Split(s, "\n")
	if len(ls) > n {
		ls = ls[:n]
	}
	return strings.Join(ls, "\n")
}
