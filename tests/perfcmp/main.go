// Command perfcmp compares two marmot builds on run time, CPU time, peak
// memory, startup, size and build time: the before/after check of SPEC 3c's
// regression rule. A and B are marmot roots (build/bin/marmot, build/lib,
// lib), or one root with different environments.
//
//	go run ./tests/perfcmp -a ROOT_BEFORE -b ROOT_AFTER
//	go run ./tests/perfcmp -a ROOT -b ROOT -b-env MARMOT_TIER0_MIR=1 -modes dev
//
// Each program is built once per side and mode, then run -runs times with
// the sides alternating (A B, B A, ...) so drift in machine load hits both.
// A line is WORSE when B is slower than A by more than 2% in wall or CPU
// time, or uses more than 3% more memory; the geomean line gives the
// verdict for the whole set. Programs: hello and a subset of bench/programs
// (time), sketch rendering its README (time), tuios --version (startup).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
)

var (
	flA     = flag.String("a", "", "marmot root before (required)")
	flB     = flag.String("b", "", "marmot root after (required; may equal -a with -a-env/-b-env)")
	flAEnv  = flag.String("a-env", "", "space separated K=V for side A's builds and runs")
	flBEnv  = flag.String("b-env", "", "space separated K=V for side B's builds and runs")
	flModes = flag.String("modes", "dev,release", "build modes: dev, O2, release")
	flRuns  = flag.Int("runs", 7, "runs per program and side")
	flProgs = flag.String("programs", "", "comma separated programs (default: hello, the bench subset, sketch, tuios)")
	flTuios = flag.String("tuios", ".work/lead/tuios-src", "tuios source tree (holds sketch/ too)")
	flWork  = flag.String("work", "", "directory for the binaries (default a new one under .work/perfcmp)")
	flKeep  = flag.Bool("keep", false, "keep the binaries")
	flWait  = flag.Duration("wait", 2*time.Hour, "how long to wait for the timing lock and a load below half the CPUs before giving up (0: no wait, no lock)")
)

const timingLock = "/tmp/marmot-timing.lock"

// benchSubset is the bench programs in the default set: compute, memory,
// maps, interfaces, goroutines and strings.
var benchSubset = []string{"nbody", "fannkuch", "bintrees", "mapstr", "iface", "pingpong", "strconcat", "quicksort", "spectralnorm", "matmul"}

type program struct {
	name, dir, pkg string
	args           []string
	kind           string // "time" or "startup"
}

type sample struct {
	wall, cpu time.Duration
	rss       int64
}

type side struct {
	label, root string
	env         []string
}

func main() {
	flag.Parse()
	if *flA == "" || *flB == "" {
		fmt.Fprintln(os.Stderr, "perfcmp: -a and -b are required")
		os.Exit(2)
	}
	repo, err := os.Getwd()
	check(err)
	sides := []side{{"A", abs(*flA), fields(*flAEnv)}, {"B", abs(*flB), fields(*flBEnv)}}
	progs := programs(repo)
	work := *flWork
	if work == "" {
		check(os.MkdirAll(filepath.Join(repo, ".work", "perfcmp"), 0o755))
		work, err = os.MkdirTemp(filepath.Join(repo, ".work", "perfcmp"), "run-")
		check(err)
	}
	if !*flKeep {
		defer os.RemoveAll(work)
	}
	sketchInput(abs(*flTuios), work)
	if *flWait > 0 {
		if !takeLock(*flWait) {
			fmt.Fprintf(os.Stderr, "perfcmp: no quiet window with %s free in %v: not timing\n", timingLock, *flWait)
			os.Exit(3)
		}
		defer os.RemoveAll(timingLock) // only a lock this run created
	}
	fmt.Printf("perfcmp %s/%s, %d runs; A=%s %s; B=%s %s\n", runtime.GOOS, runtime.GOARCH, *flRuns,
		sides[0].root, strings.Join(sides[0].env, " "), sides[1].root, strings.Join(sides[1].env, " "))
	fmt.Printf("load at start: %s\n", loadavg())
	worse := false
	for _, mode := range strings.Split(*flModes, ",") {
		fmt.Printf("\n== %s\n%-12s %10s %10s %7s %10s %10s %7s %9s %9s %7s %9s %9s %7s\n", mode,
			"program", "wall A", "wall B", "B/A", "cpu A", "cpu B", "B/A", "rss A", "rss B", "B/A", "size A", "size B", "B/A")
		var lw, lc, lr []float64
		for _, p := range progs {
			var bins [2]string
			var sizes [2]int64
			ok := true
			for i, s := range sides {
				bin, dur, err := build(s, p, mode, work)
				if err != nil {
					fmt.Printf("%-12s build %s failed: %v\n", p.name, s.label, err)
					ok = false
					break
				}
				bins[i] = bin
				if st, err := os.Stat(bin); err == nil {
					sizes[i] = st.Size()
				}
				_ = dur
			}
			if !ok {
				continue
			}
			var got [2][]sample
			runs := *flRuns
			if p.kind == "startup" {
				runs *= 5 // short runs: more samples
			}
			note := ""
			for r := 0; r < runs+1; r++ { // the first round warms caches and is dropped
				order := []int{0, 1}
				if r%2 == 1 {
					order = []int{1, 0}
				}
				for _, i := range order {
					sm, err := runOnce(sides[i], p, bins[i])
					if err != nil {
						fmt.Printf("%-12s run %s failed: %v\n", p.name, sides[i].label, err)
						ok = false
						break
					}
					if r > 0 {
						got[i] = append(got[i], sm)
					}
				}
				if !ok {
					break
				}
				if r == 1 && got[0][0].wall > 5*time.Second && runs > 1 {
					runs, note = 1, " (1 run: slow)" // a slow program in a slow mode: one sample a side
				}
			}
			if !ok {
				continue
			}
			a, b := median(got[0]), median(got[1])
			rw, rc, rr := ratio(b.wall, a.wall), ratio(b.cpu, a.cpu), float64(b.rss)/float64(a.rss)
			lw, lc, lr = append(lw, rw), append(lc, rc), append(lr, rr)
			mark := verdict(rw, rc, rr)
			fmt.Printf("%-12s %10s %10s %7.3f %10s %10s %7.3f %9s %9s %7.3f %9s %9s %7.3f %s%s\n", p.name,
				ms(a.wall), ms(b.wall), rw, ms(a.cpu), ms(b.cpu), rc, mb(a.rss), mb(b.rss), rr,
				mb(sizes[0]), mb(sizes[1]), float64(sizes[1])/float64(max(sizes[0], 1)), mark, note)
		}
		gw, gc, gr := geomean(lw), geomean(lc), geomean(lr)
		mark := verdict(gw, gc, gr)
		if mark == "WORSE" {
			worse = true
		}
		fmt.Printf("%-12s %10s %10s %7.3f %10s %10s %7.3f %9s %9s %7.3f %s\n", "geomean", "", "", gw, "", "", gc, "", "", gr, mark)
	}
	fmt.Printf("\nload at end: %s\n", loadavg())
	if l := load1(); l > float64(runtime.NumCPU())/2 {
		fmt.Printf("warning: load %.1f is above half the %d CPUs: timings are noisy, rerun on a quiet machine\n", l, runtime.NumCPU())
	}
	if worse {
		fmt.Println("verdict: WORSE (a geomean of B is more than 2% slower or 3% bigger in memory than A; lines marked WORSE show where)")
		os.Exit(1)
	}
	fmt.Println("verdict: ok (no line worse than the noise rule)")
}

func programs(repo string) []program {
	tu := abs(*flTuios)
	all := []program{{name: "hello", dir: repo, pkg: "./bench/programs/hello", kind: "startup"}}
	for _, b := range benchSubset {
		all = append(all, program{name: b, dir: repo, pkg: "./bench/programs/" + b, kind: "time"})
	}
	if _, err := os.Stat(filepath.Join(tu, "sketch", "cmd", "sketch")); err == nil {
		all = append(all, program{name: "sketch", dir: filepath.Join(tu, "sketch"), pkg: "./cmd/sketch", args: []string{"@sketch.md", "-f", "svg"}, kind: "time"})
	}
	if _, err := os.Stat(filepath.Join(tu, "cmd", "tuios")); err == nil {
		all = append(all, program{name: "tuios", dir: tu, pkg: "./cmd/tuios", args: []string{"--version"}, kind: "startup"})
	}
	if *flProgs == "" {
		return all
	}
	want := strings.Split(*flProgs, ",")
	var out []program
	for _, p := range all {
		if slices.Contains(want, p.name) {
			out = append(out, p)
		}
	}
	for _, w := range want {
		if !slices.ContainsFunc(all, func(p program) bool { return p.name == w }) && slices.Contains(benchNames(repo), w) {
			out = append(out, program{name: w, dir: repo, pkg: "./bench/programs/" + w, kind: "time"})
		}
	}
	return out
}

func benchNames(repo string) []string {
	es, _ := os.ReadDir(filepath.Join(repo, "bench", "programs"))
	var n []string
	for _, e := range es {
		n = append(n, e.Name())
	}
	return n
}

func build(s side, p program, mode, work string) (string, time.Duration, error) {
	bin := filepath.Join(work, fmt.Sprintf("%s-%s-%s", p.name, mode, s.label))
	args := []string{"build", "-o", bin}
	switch mode {
	case "release":
		args = append(args, "-release")
	case "O2":
		args = append(args, "-O", "2")
	case "dev":
	default:
		return "", 0, fmt.Errorf("unknown mode %q", mode)
	}
	args = append(args, p.pkg)
	cmd := exec.Command(filepath.Join(s.root, "build", "bin", "marmot"), args...)
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(), append([]string{"MARMOT_ROOT=" + s.root, "MARMOT_OUTPUT=plain", "CGO_ENABLED=0", "MARMOT_TIER1=0"}, s.env...)...)
	t0 := time.Now()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", 0, fmt.Errorf("%v: %s", err, lastLines(out, 4))
	}
	return bin, time.Since(t0), nil
}

func runOnce(s side, p program, bin string) (sample, error) {
	args := slices.Clone(p.args)
	for i, a := range args {
		if strings.HasPrefix(a, "@") { // a file perfcmp made in the work directory
			args[i] = filepath.Join(filepath.Dir(bin), a[1:])
		}
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(), s.env...)
	cmd.Stdout, cmd.Stderr = nil, nil
	t0 := time.Now()
	if err := cmd.Run(); err != nil {
		return sample{}, err
	}
	wall := time.Since(t0)
	ru, _ := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	var cpu time.Duration
	var rss int64
	if ru != nil {
		cpu = time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
		rss = ru.Maxrss
		if runtime.GOOS == "linux" {
			rss *= 1024 // KiB there, bytes on darwin
		}
	}
	return sample{wall, cpu, rss}, nil
}

func median(ss []sample) sample {
	w := make([]time.Duration, len(ss))
	c := make([]time.Duration, len(ss))
	r := make([]int64, len(ss))
	for i, s := range ss {
		w[i], c[i], r[i] = s.wall, s.cpu, s.rss
	}
	slices.Sort(w)
	slices.Sort(c)
	slices.Sort(r)
	return sample{w[len(w)/2], c[len(c)/2], r[len(r)/2]}
}

func ratio(b, a time.Duration) float64 { return float64(b) / float64(max(a, 1)) }

func verdict(w, c, r float64) string {
	switch {
	case w > 1.02 || c > 1.02 || r > 1.03:
		return "WORSE"
	case w < 0.98 && c < 0.98 && r <= 1.03:
		return "better"
	}
	return ""
}

func geomean(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := 0.0
	for _, x := range xs {
		s += math.Log(x)
	}
	return math.Exp(s / float64(len(xs)))
}

func ms(d time.Duration) string { return fmt.Sprintf("%.1fms", float64(d)/1e6) }
func mb(n int64) string         { return fmt.Sprintf("%.1fM", float64(n)/(1<<20)) }

func fields(s string) []string { return strings.Fields(s) }

func abs(p string) string {
	a, err := filepath.Abs(p)
	check(err)
	return a
}

func loadavg() string {
	if runtime.GOOS == "linux" {
		b, _ := os.ReadFile("/proc/loadavg")
		return strings.Join(strings.Fields(string(b))[:3], " ")
	}
	out, _ := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	return strings.Trim(strings.TrimSpace(string(out)), "{} ")
}

func lastLines(b []byte, n int) string {
	ls := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return string(bytes.Join([][]byte{[]byte(strings.Join(ls, "\n"))}, nil))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfcmp:", err)
		os.Exit(1)
	}
}

// sketchInput writes sketch.md into work: every pinned diagram of sketch's
// tests as one Markdown file, a fixed input across runs and machines.
func sketchInput(tuios, work string) {
	ms, _ := filepath.Glob(filepath.Join(tuios, "sketch", "text", "testdata", "pinned", "*.mmd"))
	var b strings.Builder
	for _, m := range ms {
		src, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "```mermaid\n%s\n```\n\n", src)
	}
	check(os.WriteFile(filepath.Join(work, "sketch.md"), []byte(b.String()), 0o644))
}

func load1() float64 {
	var l float64
	fmt.Sscanf(loadavg(), "%f", &l)
	return l
}

// takeLock waits until the load is below half the CPUs and the shared
// timing lock can be created (mkdir), then holds it: timing runs on this
// machine never overlap.
func takeLock(wait time.Duration) bool {
	end := time.Now().Add(wait)
	said := false
	for time.Now().Before(end) {
		if load1() < float64(runtime.NumCPU())/2 && os.Mkdir(timingLock, 0o755) == nil {
			_ = os.WriteFile(filepath.Join(timingLock, "owner"), []byte(fmt.Sprintf("perfcmp pid %d\n", os.Getpid())), 0o644)
			return true
		}
		if !said {
			fmt.Fprintf(os.Stderr, "perfcmp: waiting for load below %d and %s (now %.1f)\n", runtime.NumCPU()/2, timingLock, load1())
			said = true
		}
		time.Sleep(30 * time.Second)
	}
	return false
}
