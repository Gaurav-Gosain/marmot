// Command bench runs the marmot benchmark suite.
//
// For every program and toolchain it records cold and warm build time, binary size, run time,
// peak RSS and whether the output matches stock Go. Results are written as JSON.
// Everything runs serially so the measurements do not compete with each other.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Toolchain describes how to build one program with one compiler configuration.
type Toolchain struct {
	Name string
	// Build returns the argv for building pkg into out. nocache asks for a cold build where the
	// tool supports a flag for it.
	Build func(root, pkg, out string, nocache bool) []string
	// Clean returns a shell command that makes the next build cold, or "" if none is needed.
	Clean func(root, work string) string
	Flags string // human readable description of the flags, recorded in the results file
}

func marmotBin(root string) string {
	if v := os.Getenv("MARMOT_ROOT"); v != "" {
		return filepath.Join(v, "build", "bin", "marmot")
	}
	return filepath.Join(root, "build", "bin", "marmot")
}

func toolchains() []Toolchain {
	goClean := func(root, work string) string { return "rm -rf " + filepath.Join(work, "gocache") }
	tgClean := func(root, work string) string {
		return "rm -rf " + filepath.Join(work, "gocache") + "; tinygo clean"
	}
	marmot := func(opt string) func(root, pkg, out string, nocache bool) []string {
		return func(root, pkg, out string, nocache bool) []string {
			a := []string{marmotBin(root), "build", "-O", opt}
			if nocache {
				a = append(a, "-nocache")
			}
			return append(a, "-o", out, pkg)
		}
	}
	return []Toolchain{
		{Name: "go", Flags: "go build",
			Build: func(r, p, o string, _ bool) []string { return []string{"go", "build", "-o", o, p} },
			Clean: goClean},
		{Name: "go-strip", Flags: `go build -ldflags="-s -w"`,
			Build: func(r, p, o string, _ bool) []string { return []string{"go", "build", "-ldflags=-s -w", "-o", o, p} },
			Clean: goClean},
		{Name: "tinygo-O2", Flags: "tinygo build -opt=2",
			Build: func(r, p, o string, _ bool) []string { return []string{"tinygo", "build", "-opt=2", "-o", o, p} },
			Clean: tgClean},
		{Name: "tinygo-Oz", Flags: "tinygo build -opt=z",
			Build: func(r, p, o string, _ bool) []string { return []string{"tinygo", "build", "-opt=z", "-o", o, p} },
			Clean: tgClean},
		{Name: "marmot-O2", Flags: "marmot build -O 2", Build: marmot("2"),
			Clean: func(string, string) string { return "" }},
		{Name: "marmot-Oz", Flags: "marmot build -O z", Build: marmot("z"),
			Clean: func(string, string) string { return "" }},
	}
}

// Stat is a summary of repeated timings, in milliseconds.
type Stat struct {
	Min    float64 `json:"min"`
	Median float64 `json:"median"`
	Mean   float64 `json:"mean"`
	Stddev float64 `json:"stddev"`
	Runs   int     `json:"runs"`
}

// Result is one program on one toolchain.
type Result struct {
	Program      string `json:"program"`
	Tier         string `json:"tier"`
	Toolchain    string `json:"toolchain"`
	Status       string `json:"status"` // ok, build_failed, run_failed, mismatch
	Error        string `json:"error,omitempty"`
	BuildCold    *Stat  `json:"build_cold_ms,omitempty"`
	BuildWarm    *Stat  `json:"build_warm_ms,omitempty"`
	Size         int64  `json:"size_bytes,omitempty"`
	SizeStripped int64  `json:"size_stripped_bytes,omitempty"`
	Run          *Stat  `json:"run_ms,omitempty"`
	RSS          int64  `json:"peak_rss_bytes,omitempty"`
	OutputMatch  *bool  `json:"output_match,omitempty"`
}

// Meta records the environment so a results file stands on its own.
type Meta struct {
	Date       string            `json:"date"`
	Label      string            `json:"label"`
	CPU        string            `json:"cpu"`
	NCPU       int               `json:"ncpu"`
	MemBytes   int64             `json:"mem_bytes"`
	OS         string            `json:"os"`
	Arch       string            `json:"arch"`
	LoadAvg    string            `json:"load_avg_at_start"`
	Versions   map[string]string `json:"versions"`
	Flags      map[string]string `json:"flags"`
	RunRuns    int               `json:"run_runs"`
	WarmRuns   int               `json:"warm_build_runs"`
	ColdRuns   int               `json:"cold_build_runs"`
	GitRev     string            `json:"git_rev"`
	Hyperfine  string            `json:"hyperfine"`
	Notes      string            `json:"notes"`
	TimeoutSec int               `json:"program_timeout_sec"`
}

// File is the JSON document.
type File struct {
	Meta    Meta     `json:"meta"`
	Results []Result `json:"results"`
}

var (
	root       string
	work       string
	runRuns    = flag.Int("runs", 10, "run time samples per program (hyperfine)")
	warmRuns   = flag.Int("warm-runs", 5, "warm build samples")
	coldRuns   = flag.Int("cold-runs", 3, "cold build samples")
	programs   = flag.String("programs", "", "comma separated program names (default: all)")
	tiers      = flag.String("tiers", "", "comma separated tiers: nostd,std (default: both)")
	tcs        = flag.String("toolchains", "", "comma separated toolchains (default: all)")
	label      = flag.String("label", "baseline", "label used in the results file name")
	outDir     = flag.String("out", "", "results directory (default bench/results)")
	skipCold   = flag.Bool("skip-cold", false, "skip cold build timing")
	updateExp  = flag.Bool("update-expected", false, "rewrite bench/expected/ from stock go output")
	timeoutSec = flag.Int("timeout", 120, "kill a program after this many seconds")
	listOnly   = flag.Bool("list", false, "list programs and toolchains and exit")
	notes      = flag.String("notes", "", "free text stored in the results file")
	noSnapshot = flag.Bool("no-snapshot", false, "use the live tree for marmot instead of a snapshot (do not use for long runs)")
)

func main() {
	flag.Parse()
	var err error
	root, err = findRoot()
	check(err)
	work = filepath.Join(root, ".work", "bench")
	check(os.MkdirAll(work, 0o755))

	progs := discover()
	if *programs != "" {
		progs = filter(progs, strings.Split(*programs, ","), func(p Program) string { return p.Name })
	}
	if *tiers != "" {
		progs = filter(progs, strings.Split(*tiers, ","), func(p Program) string { return p.Tier })
	}
	tcl := toolchains()
	if *tcs != "" {
		tcl = filter(tcl, strings.Split(*tcs, ","), func(t Toolchain) string { return t.Name })
	}
	if *listOnly {
		for _, p := range progs {
			fmt.Println(p.Tier, p.Name)
		}
		for _, t := range tcl {
			fmt.Println("toolchain", t.Name, "=", t.Flags)
		}
		return
	}
	if *updateExp {
		updateExpected(progs)
		return
	}

	usesMarmot := false
	for _, t := range tcl {
		if strings.HasPrefix(t.Name, "marmot") {
			usesMarmot = true
		}
	}
	if usesMarmot && !*noSnapshot {
		makeSnapshot()
	}
	meta := collectMeta(tcl)
	file := File{Meta: meta}
	dir := *outDir
	if dir == "" {
		dir = filepath.Join(root, "bench", "results")
	}
	check(os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, meta.Date+"-"+*label+".json")

	for _, p := range progs {
		for _, t := range tcl {
			fmt.Fprintf(os.Stderr, "== %s / %s\n", p.Name, t.Name)
			r := measure(p, t)
			fmt.Fprintf(os.Stderr, "   %s run=%s size=%d\n", r.Status, fmtStat(r.Run), r.Size)
			file.Results = append(file.Results, r)
			// Write after every result so a crash or ctrl-c keeps the partial data.
			writeJSON(path, file)
		}
	}
	fmt.Fprintln(os.Stderr, "wrote", path)
}

// Program is one benchmark directory.
type Program struct {
	Name string
	Tier string
	Pkg  string
}

var importRe = regexp.MustCompile(`(?m)^\s*import\b`)

func discover() []Program {
	entries, err := os.ReadDir(filepath.Join(root, "bench", "programs"))
	check(err)
	var out []Program
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, "bench", "programs", e.Name(), "main.go"))
		if err != nil {
			continue
		}
		tier := "nostd"
		if importRe.Match(src) {
			tier = "std"
		}
		out = append(out, Program{e.Name(), tier, "./bench/programs/" + e.Name()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier < out[j].Tier
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func filter[T any](in []T, names []string, key func(T) string) []T {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.TrimSpace(n)] = true
	}
	var out []T
	for _, v := range in {
		if want[key(v)] {
			out = append(out, v)
		}
	}
	return out
}

func measure(p Program, t Toolchain) Result {
	r := Result{Program: p.Name, Tier: p.Tier, Toolchain: t.Name}
	dir := filepath.Join(work, "out", t.Name)
	check(os.MkdirAll(dir, 0o755))
	out := filepath.Join(dir, p.Name)
	os.Remove(out)
	env := append(os.Environ(), "GOCACHE="+filepath.Join(work, "gocache"))

	// Cold build. Each sample cleans first, so the stdlib is rebuilt too.
	if !*skipCold {
		clean := t.Clean(root, work)
		argv := t.Build(root, p.Pkg, out, true)
		if st, err := hyperfineBuild(argv, clean, *coldRuns, 0, env); err != nil {
			r.Status, r.Error = "build_failed", trunc(err.Error())
			return r
		} else {
			r.BuildCold = st
		}
	}
	// Warm build. One warmup populates the caches.
	argv := t.Build(root, p.Pkg, out, false)
	st, err := hyperfineBuild(argv, "", *warmRuns, 1, env)
	if err != nil {
		r.Status, r.Error = "build_failed", trunc(err.Error())
		return r
	}
	r.BuildWarm = st
	fi, err := os.Stat(out)
	if err != nil {
		r.Status, r.Error = "build_failed", "no output binary"
		return r
	}
	r.Size = fi.Size()
	r.SizeStripped = strippedSize(out)

	// Output check against the stock Go reference.
	got, err := runOnce(out)
	if err != nil {
		r.Status, r.Error = "run_failed", trunc(err.Error()+"\n"+got)
		return r
	}
	want, _ := os.ReadFile(filepath.Join(root, "bench", "expected", p.Name+".txt"))
	m := string(want) == got
	r.OutputMatch = &m
	if !m {
		r.Status, r.Error = "mismatch", trunc("got:\n"+got+"\nwant:\n"+string(want))
		return r
	}

	if r.Run, err = hyperfineRun(out, *runRuns); err != nil {
		r.Status, r.Error = "run_failed", trunc(err.Error())
		return r
	}
	r.RSS = peakRSS(out)
	r.Status = "ok"
	return r
}

// runOnce runs the binary and returns stdout and stderr in a labelled form so that println
// (stderr) output is compared as well.
func runOnce(bin string) (string, error) {
	cmd := exec.Command(bin)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	_, err := runGroup(cmd, time.Duration(*timeoutSec)*time.Second)
	// Go's println writes to stderr, TinyGo's to stdout. Compare the two streams joined so
	// the destination does not matter.
	return so.String() + se.String(), err
}

func hyperfineBuild(argv []string, clean string, runs, warmup int, env []string) (*Stat, error) {
	// First do one plain build to surface errors with readable output.
	c := exec.Command(argv[0], argv[1:]...)
	c.Dir, c.Env = root, env
	if clean != "" {
		cc := exec.Command("sh", "-c", clean)
		cc.Dir, cc.Env = root, env
		runGroup(cc, 2*time.Minute)
	}
	if out, err := runGroup(c, 10*time.Minute); err != nil {
		return nil, fmt.Errorf("%v: %s", err, out)
	}
	args := []string{"--runs", strconv.Itoa(runs), "--warmup", strconv.Itoa(warmup), "--style", "none", "-N"}
	if clean != "" {
		args = append(args, "--prepare", clean)
	}
	return hyperfine(args, shellJoin(argv), env, root)
}

func hyperfineRun(bin string, runs int) (*Stat, error) {
	return hyperfine([]string{"--runs", strconv.Itoa(runs), "--warmup", "1", "--style", "none", "-N", "--output=null"}, bin, os.Environ(), root)
}

func hyperfine(args []string, command string, env []string, dir string) (*Stat, error) {
	tmp := filepath.Join(work, "hf.json")
	os.Remove(tmp)
	args = append(args, "--export-json", tmp, command)
	c := exec.Command("hyperfine", args...)
	c.Dir, c.Env = dir, env
	// Generous bound: the longest legitimate hyperfine call is a few minutes.
	if out, err := runGroup(c, 15*time.Minute); err != nil {
		return nil, fmt.Errorf("hyperfine: %v: %s", err, out)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		return nil, err
	}
	// hyperfine 2.x schema: results[0].summary.time_wall_clock.
	var doc struct {
		Results []struct {
			Summary struct {
				Wall struct {
					Count                     int
					Mean, Stddev, Median, Min float64
				} `json:"time_wall_clock"`
			}
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil || len(doc.Results) == 0 || doc.Results[0].Summary.Wall.Count == 0 {
		return nil, fmt.Errorf("bad hyperfine output")
	}
	h := doc.Results[0].Summary.Wall
	return &Stat{Min: h.Min * 1000, Median: h.Median * 1000, Mean: h.Mean * 1000, Stddev: h.Stddev * 1000, Runs: h.Count}, nil
}

func shellJoin(argv []string) string {
	var q []string
	for _, a := range argv {
		if strings.ContainsAny(a, " \t\"'") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		q = append(q, a)
	}
	return strings.Join(q, " ")
}

func strippedSize(bin string) int64 {
	tmp := bin + ".stripped"
	data, err := os.ReadFile(bin)
	if err != nil || os.WriteFile(tmp, data, 0o755) != nil {
		return 0
	}
	defer os.Remove(tmp)
	if exec.Command("strip", tmp).Run() != nil {
		return 0
	}
	fi, err := os.Stat(tmp)
	if err != nil {
		return 0
	}
	return fi.Size()
}

var rssRe = regexp.MustCompile(`(\d+)\s+maximum resident set size`)

// peakRSS returns the median of three /usr/bin/time -l measurements, in bytes.
func peakRSS(bin string) int64 {
	var v []int64
	for i := 0; i < 3; i++ {
		var se bytes.Buffer
		c := exec.Command("/usr/bin/time", "-l", bin)
		c.Stderr = &se
		runGroup(c, time.Duration(*timeoutSec)*time.Second)
		if m := rssRe.FindSubmatch(se.Bytes()); m != nil {
			n, _ := strconv.ParseInt(string(m[1]), 10, 64)
			v = append(v, n)
		}
	}
	if len(v) == 0 {
		return 0
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v[len(v)/2]
}

func updateExpected(progs []Program) {
	dir := filepath.Join(root, "bench", "expected")
	check(os.MkdirAll(dir, 0o755))
	for _, p := range progs {
		out := filepath.Join(work, "ref-"+p.Name)
		b := exec.Command("go", "build", "-o", out, p.Pkg)
		b.Dir = root
		if o, err := b.CombinedOutput(); err != nil {
			check(fmt.Errorf("%s: %v\n%s", p.Name, err, o))
		}
		// Run twice: a program with nondeterministic output cannot be a benchmark.
		a, err := runOnce(out)
		check(err)
		b2, _ := runOnce(out)
		if a != b2 {
			check(fmt.Errorf("%s: nondeterministic output", p.Name))
		}
		check(os.WriteFile(filepath.Join(dir, p.Name+".txt"), []byte(a), 0o644))
		fmt.Println("expected", p.Name)
	}
}

func collectMeta(tcl []Toolchain) Meta {
	m := Meta{
		Date: time.Now().Format("2006-01-02"), Label: *label, NCPU: runtime.NumCPU(),
		OS: runtime.GOOS, Arch: runtime.GOARCH, RunRuns: *runRuns, WarmRuns: *warmRuns,
		ColdRuns: *coldRuns, Notes: *notes, TimeoutSec: *timeoutSec,
		Versions: map[string]string{}, Flags: map[string]string{},
	}
	if *skipCold {
		m.ColdRuns = 0
	}
	m.CPU = sh("sysctl", "-n", "machdep.cpu.brand_string")
	m.MemBytes, _ = strconv.ParseInt(sh("sysctl", "-n", "hw.memsize"), 10, 64)
	m.OS = sh("sw_vers", "-productName") + " " + sh("sw_vers", "-productVersion")
	m.LoadAvg = sh("sysctl", "-n", "vm.loadavg")
	m.GitRev = sh("git", "-C", root, "rev-parse", "--short", "HEAD")
	if snapDir != "" {
		m.GitRev += " (snapshot " + filepath.Base(snapDir) + ")"
	}
	m.Hyperfine = sh("hyperfine", "--version")
	m.Versions["go"] = sh("go", "version")
	m.Versions["tinygo"] = sh("tinygo", "version")
	if v := sh(marmotBin(root), "version"); v != "" {
		m.Versions["marmot"] = v
	} else {
		m.Versions["marmot"] = "not built"
	}
	for _, t := range tcl {
		m.Flags[t.Name] = t.Flags
	}
	return m
}

func sh(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func writeJSON(path string, f File) {
	data, err := json.MarshalIndent(f, "", "  ")
	check(err)
	check(os.WriteFile(path, append(data, '\n'), 0o644))
}

func fmtStat(s *Stat) string {
	if s == nil {
		return "-"
	}
	return fmt.Sprintf("%.1fms", s.Median)
}

func trunc(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "..."
	}
	return s
}

func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := dir; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "SPEC.md")); err == nil {
			return d, nil
		}
	}
	return "", fmt.Errorf("cannot find repo root from %s", dir)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}

// runGroup runs c in its own process group and kills the whole group when the timeout passes
// or when the command ends, so no child (compiler backend, benchmark program) can be orphaned.
func runGroup(c *exec.Cmd, timeout time.Duration) ([]byte, error) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var buf bytes.Buffer
	if c.Stdout == nil {
		c.Stdout = &buf
	}
	if c.Stderr == nil {
		c.Stderr = &buf
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		err = fmt.Errorf("timeout after %v", timeout)
		syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		<-done
	}
	syscall.Kill(-c.Process.Pid, syscall.SIGKILL) // reap stragglers
	return buf.Bytes(), err
}

var snapDir string

// makeSnapshot copies everything marmot needs (build/bin, build/lib, lib) into
// .work/bench/snap-<sha> and points MARMOT_ROOT at it, so other workers editing the live tree
// cannot break a long run. Older snapshots are deleted. A hello world smoke test must pass.
func makeSnapshot() {
	sha := sh("git", "-C", root, "rev-parse", "--short", "HEAD")
	if sh("git", "-C", root, "status", "--porcelain", "--", "lib", "cmd", "internal", "backend", "runtime") != "" {
		sha += "-dirty"
	}
	old, _ := filepath.Glob(filepath.Join(work, "snap-*"))
	for _, o := range old {
		os.RemoveAll(o)
	}
	snapDir = filepath.Join(work, "snap-"+sha)
	for _, d := range [][2]string{{"build/bin", "build/bin"}, {"build/lib", "build/lib"}, {"lib", "lib"}} {
		dst := filepath.Join(snapDir, d[1])
		check(os.MkdirAll(filepath.Dir(dst), 0o755))
		if out, err := exec.Command("cp", "-R", filepath.Join(root, d[0]), dst).CombinedOutput(); err != nil {
			check(fmt.Errorf("snapshot copy %s: %v %s", d[0], err, out))
		}
	}
	os.Setenv("MARMOT_ROOT", snapDir)
	fmt.Fprintln(os.Stderr, "snapshot", snapDir)

	// Smoke test: build and run hello against the snapshot.
	out := filepath.Join(work, "smoke-hello")
	c := exec.Command(marmotBin(root), "build", "-o", out, "./bench/programs/hello")
	c.Dir, c.Env = root, os.Environ()
	if o, err := runGroup(c, 5*time.Minute); err != nil {
		check(fmt.Errorf("snapshot smoke build failed: %v\n%s", err, o))
	}
	got, err := runOnce(out)
	want, _ := os.ReadFile(filepath.Join(root, "bench", "expected", "hello.txt"))
	if err != nil || got != string(want) {
		check(fmt.Errorf("snapshot smoke run failed: %v got %q", err, got))
	}
	os.Remove(out)
}
