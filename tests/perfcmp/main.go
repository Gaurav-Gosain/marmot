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
// (time), sketch rendering its pinned diagrams (time), tuios --version
// (startup).
//
// A verdict needs a machine quiet enough to see 2%. Each mode prints a noise
// line, each side against itself (its odd runs over its even ones); with A=B
// the whole result is noise too. When a noise line is above 1.02, or the load
// went above half the CPUs at any point, the verdict is INCONCLUSIVE instead
// of WORSE or ok. A build or run that fails on either side, a ratio that is
// not a number, or a -programs name that is not here makes it FAILED, which
// comes before everything else: a comparison with a hole in it is never ok.
//
// Exit status: 0 ok, 1 WORSE, 2 usage, 3 no quiet window in -wait,
// 4 INCONCLUSIVE, 5 FAILED.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	flTuios = flag.String("tuios", envOr("MARMOT_TUIOS_SRC", ".work/lead/tuios-src"), "tuios source tree (holds sketch/ too; default $MARMOT_TUIOS_SRC)")
	flWork  = flag.String("work", "", "the binaries go in a new directory inside this one (default .work/perfcmp)")
	flKeep  = flag.Bool("keep", false, "keep the binaries")
	flWait  = flag.Duration("wait", 2*time.Hour, "how long to wait for the timing lock and a load below half the CPUs before giving up (0: no wait, no lock)")
)

var timingLock = "/tmp/marmot-timing.lock" // a var for the tests

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
		os.Exit(exitUsage)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		sig := <-sigs
		fmt.Fprintf(os.Stderr, "perfcmp: %v: cleaning up\n", sig)
		exit(128 + int(sig.(syscall.Signal)))
	}()
	repo, err := os.Getwd()
	check(err)
	sides := []side{{"A", abs(*flA), fields(*flAEnv)}, {"B", abs(*flB), fields(*flBEnv)}}
	progs := programs(repo)
	// A -work directory is the caller's: the binaries go in a new directory inside it, and
	// only that one is removed.
	parent := *flWork
	if parent == "" {
		parent = filepath.Join(repo, ".work", "perfcmp")
	}
	check(os.MkdirAll(parent, 0o755))
	work, err := os.MkdirTemp(parent, "run-")
	check(err)
	if !*flKeep {
		atExit(func() { os.RemoveAll(work) })
	}
	sketchInput(abs(*flTuios), work)
	if *flWait > 0 {
		if !takeLock(*flWait) {
			fmt.Fprintf(os.Stderr, "perfcmp: no quiet window with %s free in %v: not timing\n", timingLock, *flWait)
			exit(exitNoWindow)
		}
		atExit(dropLock)
	}
	fmt.Printf("perfcmp %s/%s, %d runs; A=%s %s; B=%s %s\n", runtime.GOOS, runtime.GOARCH, *flRuns,
		sides[0].root, strings.Join(sides[0].env, " "), sides[1].root, strings.Join(sides[1].env, " "))
	fmt.Printf("load at start: %s\n", loadavg())
	fmt.Println("rss: each binary runs as a fresh copy (new file, out of the page cache); rss from perfcmp before this is suspect")
	same := sides[0].root == sides[1].root && slices.Equal(sides[0].env, sides[1].env)
	var o outcome
	o.same = same
	for _, m := range missingProgs {
		o.failed = append(o.failed, m+": no such program here")
	}
	maxLoad := load1()
	sampleLoad := func() {
		if l := load1(); l > maxLoad {
			maxLoad = l
		}
	}
	for _, mode := range strings.Split(*flModes, ",") {
		fmt.Printf("\n== %s\n%-12s %10s %10s %7s %10s %10s %7s %9s %9s %7s %9s %9s %7s\n", mode,
			"program", "wall A", "wall B", "B/A", "cpu A", "cpu B", "B/A", "rss A", "rss B", "B/A", "size A", "size B", "B/A")
		var lw, lc, lr []float64
		var nw, nc []float64 // per side and program: median of the odd runs over the even ones
		for _, p := range progs {
			sampleLoad()
			var bins [2]string
			var sizes [2]int64
			ok := true
			for i, s := range sides {
				bin, dur, err := build(s, p, mode, work)
				if err != nil {
					fmt.Printf("%-12s build %s failed: %v\n", p.name, s.label, err)
					o.failed = append(o.failed, fmt.Sprintf("%s %s: build %s failed", p.name, mode, s.label))
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
						o.failed = append(o.failed, fmt.Sprintf("%s %s: run %s failed", p.name, mode, sides[i].label))
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
			sampleLoad()
			for i := range got {
				if len(got[i]) >= 4 {
					ev, od := halves(got[i])
					nw, nc = append(nw, ratio(od.wall, ev.wall)), append(nc, ratio(od.cpu, ev.cpu))
				}
			}
			a, b := median(got[0]), median(got[1])
			rw, rc, rr := ratio(b.wall, a.wall), ratio(b.cpu, a.cpu), float64(b.rss)/float64(a.rss)
			if bad := notFinite(rw, rc, rr); bad != "" {
				o.failed = append(o.failed, fmt.Sprintf("%s %s: %s ratio is not a number", p.name, mode, bad))
			}
			lw, lc, lr = append(lw, rw), append(lc, rc), append(lr, rr)
			mark := verdict(rw, rc, rr)
			fmt.Printf("%-12s %10s %10s %7.3f %10s %10s %7.3f %9s %9s %7.3f %9s %9s %7.3f %s%s\n", p.name,
				ms(a.wall), ms(b.wall), rw, ms(a.cpu), ms(b.cpu), rc, mb(a.rss), mb(b.rss), rr,
				mb(sizes[0]), mb(sizes[1]), float64(sizes[1])/float64(max(sizes[0], 1)), mark, note)
		}
		gw, gc, gr := geomean(lw), geomean(lc), geomean(lr)
		if len(lw) == 0 {
			o.failed = append(o.failed, fmt.Sprintf("%s: no program measured on both sides", mode))
		} else if bad := notFinite(gw, gc, gr); bad != "" {
			o.failed = append(o.failed, fmt.Sprintf("%s: the %s geomean is not a number", mode, bad))
		}
		mark := verdict(gw, gc, gr)
		if mark == "WORSE" {
			o.worse = true
		}
		fmt.Printf("%-12s %10s %10s %7.3f %10s %10s %7.3f %9s %9s %7.3f %s\n", "geomean", "", "", gw, "", "", gc, "", "", gr, mark)
		// The noise of this mode: each side against itself, the odd runs over the even
		// ones, geomean over programs, scaled to the full medians the verdict compares
		// (half the runs a side: sqrt 2 more spread). With A=B the result is noise too.
		ew, ec := noiseOf(nw), noiseOf(nc)
		if same {
			ew, ec = max(ew, spread(gw)), max(ec, spread(gc))
		}
		fmt.Printf("%-12s %10s %10s %7.3f %10s %10s %7.3f\n", "noise", "", "", ew, "", "", ec)
		if ew > 1.02 || ec > 1.02 {
			o.noisy = true
		}
	}
	fmt.Printf("\nload at end: %s\n", loadavg())
	sampleLoad()
	o.maxLoad, o.cpus = maxLoad, runtime.NumCPU()
	if o.maxLoad > float64(o.cpus)/2 {
		fmt.Printf("warning: load reached %.1f, above half the %d CPUs: timings are noisy, rerun on a quiet machine\n", o.maxLoad, o.cpus)
	}
	line, code := o.verdict()
	fmt.Println(line)
	exit(code)
}

// Exit statuses.
const (
	exitOK           = 0
	exitWorse        = 1
	exitUsage        = 2
	exitNoWindow     = 3
	exitInconclusive = 4
	exitFailed       = 5
)

// outcome is what a run found; verdict turns it into the last line and the exit status.
type outcome struct {
	failed             []string // builds or runs that failed, ratios that are not numbers
	worse, noisy, same bool
	maxLoad            float64
	cpus               int
}

// verdict: FAILED before anything else (a comparison with a hole in it is never ok), then
// INCONCLUSIVE (too loaded or too noisy to judge 2%), then WORSE, then ok.
func (o outcome) verdict() (string, int) {
	if len(o.failed) > 0 {
		return fmt.Sprintf("verdict: FAILED (%d %s, so B was not judged against A: %s)", len(o.failed), plural(len(o.failed), "failure", "failures"), strings.Join(o.failed, "; ")), exitFailed
	}
	loaded := o.maxLoad > float64(o.cpus)/2
	if loaded || o.noisy {
		why := []string{}
		if loaded {
			why = append(why, fmt.Sprintf("the load reached %.1f on %d CPUs", o.maxLoad, o.cpus))
		}
		if o.noisy {
			why = append(why, "the noise line of a mode is above 1.02, the 2% the rule judges")
		}
		would := "ok"
		if o.worse {
			would = "WORSE"
		}
		return fmt.Sprintf("verdict: INCONCLUSIVE (%s; this run cannot judge the 2%% rule, it would have said %s; rerun on a quiet machine, the Linux host through at.sh)", strings.Join(why, ", and "), would), exitInconclusive
	}
	if o.same {
		return "verdict: ok (A=B: the noise is within the rule, this machine can judge it now)", exitOK
	}
	if o.worse {
		return "verdict: WORSE (a geomean of B is more than 2% slower or 3% bigger in memory than A; lines marked WORSE show where)", exitWorse
	}
	return "verdict: ok (no line worse than the noise rule)", exitOK
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// notFinite names the first of wall, cpu, rss that is NaN or infinite, or "".
func notFinite(w, c, r float64) string {
	for i, x := range []float64{w, c, r} {
		if math.IsNaN(x) || math.IsInf(x, 0) || x <= 0 {
			return [...]string{"wall", "cpu", "rss"}[i]
		}
	}
	return ""
}

// cleanups run on every exit: the end of main, exit, check and a signal.
// os.Exit skips deferred calls, so nothing here relies on defer.
var (
	cleanMu  sync.Mutex
	cleanups []func()
)

func atExit(f func()) {
	cleanMu.Lock()
	cleanups = append(cleanups, f)
	cleanMu.Unlock()
}

func exit(code int) {
	cleanMu.Lock()
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
	cleanups = nil
	os.Exit(code)
}

// dropLock removes the timing lock if this process holds it.
func dropLock() {
	b, err := os.ReadFile(filepath.Join(timingLock, "owner"))
	if err == nil && string(b) == lockOwner() {
		os.RemoveAll(timingLock)
	}
}

func lockOwner() string { return fmt.Sprintf("perfcmp pid %d\n", os.Getpid()) }

// missingProgs are -programs names perfcmp cannot find (no such bench program, or
// sketch and tuios without a tuios tree); each makes the verdict FAILED.
var missingProgs []string

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
	} else {
		fmt.Fprintf(os.Stderr, "perfcmp: no tuios tree at %s: sketch and tuios are left out (set -tuios or MARMOT_TUIOS_SRC)\n", tu)
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
		if !slices.ContainsFunc(all, func(p program) bool { return p.name == w }) {
			if slices.Contains(benchNames(repo), w) {
				out = append(out, program{name: w, dir: repo, pkg: "./bench/programs/" + w, kind: "time"})
			} else {
				missingProgs = append(missingProgs, w)
			}
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
	d := time.Since(t0)
	if err := freshCopy(bin); err != nil {
		return "", 0, fmt.Errorf("fresh copy of %s: %v", bin, err)
	}
	return bin, d, nil
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

// halves splits a side's runs into the even and odd ones and returns their medians.
func halves(ss []sample) (even, odd sample) {
	var e, o []sample
	for i, s := range ss {
		if i%2 == 0 {
			e = append(e, s)
		} else {
			o = append(o, s)
		}
	}
	return median(e), median(o)
}

// noiseOf turns self ratios into a noise factor at least 1: the geomean of their
// spread, scaled by 1/sqrt 2 since each half has half the runs.
func noiseOf(rs []float64) float64 {
	if len(rs) == 0 {
		return 1
	}
	s := 0.0
	for _, r := range rs {
		s += math.Abs(math.Log(r))
	}
	return math.Exp(s / float64(len(rs)) / math.Sqrt2)
}

// spread is a ratio's distance from 1 as a factor at least 1.
func spread(r float64) float64 { return math.Exp(math.Abs(math.Log(r))) }

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

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

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
		fmt.Println("verdict: FAILED (perfcmp stopped: " + err.Error() + ")")
		exit(exitFailed)
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

// healLock clears a timing lock its owner left behind: the owner file's last
// word is the holder's pid, and when no such process exists (kill -0 fails)
// the lock is moved aside and the takeover logged. A lock with no owner file
// is stale after a minute (the holder writes it right after mkdir). The move
// is a rename, so of two runs healing at once only one moves it; a run that
// finds it moved a lock someone took meanwhile puts it back.
func healLock() {
	fi, err := os.Stat(timingLock)
	if err != nil {
		return
	}
	owner, err := os.ReadFile(filepath.Join(timingLock, "owner"))
	why := ""
	switch {
	case err != nil && time.Since(fi.ModTime()) > time.Minute:
		why = "no owner file for " + time.Since(fi.ModTime()).Round(time.Second).String()
	case err == nil:
		f := strings.Fields(string(owner))
		if len(f) == 0 {
			return
		}
		pid, perr := strconv.Atoi(f[len(f)-1])
		if perr != nil || pid <= 0 || syscall.Kill(pid, 0) != syscall.ESRCH {
			return // not a pid, or the process exists (or belongs to someone else)
		}
		why = fmt.Sprintf("owner %q is gone", strings.TrimSpace(string(owner)))
	default:
		return
	}
	aside := fmt.Sprintf("%s.stale.%d", timingLock, os.Getpid())
	if os.Rename(timingLock, aside) != nil {
		return // someone else moved or released it first
	}
	if now, _ := os.ReadFile(filepath.Join(aside, "owner")); string(now) != string(owner) {
		_ = os.Rename(aside, timingLock) // a fresh lock replaced the stale one: not ours to move
		return
	}
	os.RemoveAll(aside)
	fmt.Fprintf(os.Stderr, "perfcmp: took over the stale timing lock %s: %s\n", timingLock, why)
}

// takeLock waits until the load is below half the CPUs and the shared
// timing lock can be created (mkdir), then holds it: timing runs on this
// machine never overlap.
func takeLock(wait time.Duration) bool {
	end := time.Now().Add(wait)
	said := false
	for time.Now().Before(end) {
		healLock()
		if load1() < float64(runtime.NumCPU())/2 && os.Mkdir(timingLock, 0o755) == nil {
			_ = os.WriteFile(filepath.Join(timingLock, "owner"), []byte(lockOwner()), 0o644)
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
