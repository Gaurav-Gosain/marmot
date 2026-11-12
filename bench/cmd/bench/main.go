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
	"math"
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
	// Env holds extra KEY=VALUE variables for the build command only.
	Env   []string
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
	// dev is the default mode. release adds -release once the driver supports it; until then the
	// default build (whole program step on) counts as release, as the maintainer decided.
	releaseFlag := func(root string) []string {
		out, _ := exec.Command(marmotBin(root), "build", "-h").CombinedOutput()
		if bytes.Contains(out, []byte("-release")) {
			return []string{"-release"}
		}
		return nil
	}
	marmotMode := func(release bool) func(root, pkg, out string, nocache bool) []string {
		return func(root, pkg, out string, nocache bool) []string {
			a := []string{marmotBin(root), "build"}
			if release {
				a = append(a, releaseFlag(root)...)
			}
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
		{Name: "marmot-dev", Flags: "marmot build (dev mode, default flags)", Build: marmotMode(false),
			Clean: func(string, string) string { return "" }},
		{Name: "marmot-release", Flags: "marmot build -release (default flags until -release exists)", Build: marmotMode(true),
			Clean: func(string, string) string { return "" }},
		{Name: "marmot-release-loops", Flags: `marmot build -release with MARMOT_BACKEND_FLAGS="-par -type-alias"`, Build: marmotMode(true),
			Env:   []string{"MARMOT_BACKEND_FLAGS=-par -type-alias"},
			Clean: func(string, string) string { return "" }},
		{Name: "marmot-release-spec", Flags: "marmot build -release with MARMOT_SPEC=all", Build: marmotMode(true),
			Env:   []string{"MARMOT_SPEC=all"},
			Clean: func(string, string) string { return "" }},
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
	// CPU is the median user plus system CPU time in milliseconds. It exceeds the wall time when
	// the program uses more than one core.
	CPU float64 `json:"cpu_median,omitempty"`
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
	// Load is the one minute load average when this program was measured.
	Load float64 `json:"load_avg_1m"`
	// LoadEnd is the load average when the measurement ended. Noisy is set when either value was
	// above -max-load after all retries; the report drops the timing of noisy rows.
	LoadEnd float64 `json:"load_avg_1m_end"`
	Noisy   bool    `json:"noisy,omitempty"`
	// Rev is the marmot commit (and snapshot) this row was measured with.
	Rev string `json:"rev,omitempty"`
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
	runRuns    = flag.Int("runs", 10, "run time samples per program ")
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
	maxLoad    = flag.Float64("max-load", 12, "retry a program when the load average is above this at start or end; 0 disables")
	notes      = flag.String("notes", "", "free text stored in the results file")
	sizeOnly   = flag.Bool("size-only", false, "build once and record size and output check only, no timing")
	resume     = flag.Bool("resume", false, "continue the results file of this label and date: skip program and toolchain pairs already measured without noise")
	redo       = flag.String("redo", "", "with -resume: comma separated programs whose old rows are discarded and measured again")
	redoMarmot = flag.Bool("redo-marmot", false, "with -resume: discard every marmot row so all marmot toolchains are measured again (go and tinygo rows are kept)")
	prune      = flag.Bool("prune", false, "skip marmot-release-spec for nostd programs (identical to release there) and marmot-release-loops for std programs that are not class A")
	noSnapshot = flag.Bool("no-snapshot", false, "use the live tree for marmot instead of a snapshot (do not use for long runs)")
)

func main() {
	flag.Parse()
	var err error
	root, err = findRoot()
	check(err)
	work = filepath.Join(root, ".work", "bench", *label)
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

	lockBenchmarks()
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
	benchRev = meta.GitRev
	file := File{Meta: meta}
	dir := *outDir
	if dir == "" {
		dir = filepath.Join(root, "bench", "results")
	}
	check(os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, meta.Date+"-"+*label+".json")

	done := map[string]bool{}
	if *resume {
		if data, err := os.ReadFile(path); err == nil {
			var old File
			if json.Unmarshal(data, &old) == nil {
				redoSet := map[string]bool{}
				for _, n := range strings.Split(*redo, ",") {
					redoSet[strings.TrimSpace(n)] = true
				}
				for _, r := range old.Results {
					if redoSet[r.Program] || (*redoMarmot && strings.HasPrefix(r.Toolchain, "marmot")) {
						continue // superseded: measured again at the new revision
					}
					if r.Status == "ok" && !r.Noisy {
						file.Results = append(file.Results, r)
						done[r.Program+"|"+r.Toolchain] = true
					}
				}
				file.Meta.Notes = old.Meta.Notes + " Resumed at " + file.Meta.GitRev + " with all build slots held while timing."
			}
		}
	}
	for _, p := range progs {
		for _, t := range tcl {
			if done[p.Name+"|"+t.Name] || (*prune && pruned(p, t)) {
				continue
			}
			fmt.Fprintf(os.Stderr, "== %s / %s\n", p.Name, t.Name)
			r := measureQuiet(p, t)
			fmt.Fprintf(os.Stderr, "   %s run=%s size=%d\n", r.Status, fmtStat(r.Run), r.Size)
			file.Results = append(file.Results, r)
			// Write after every result so a crash or ctrl-c keeps the partial data.
			writeJSON(path, file)
		}
	}
	fmt.Fprintln(os.Stderr, "wrote", path)
	// Keep scratch small: drop the snapshot, binaries and caches of this run.
	for _, d := range []string{snapDir, filepath.Join(work, "out"), filepath.Join(work, "gocache")} {
		if d != "" {
			os.RemoveAll(d)
		}
	}
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
	r := Result{Program: p.Name, Tier: p.Tier, Toolchain: t.Name, Load: loadAvg1()}
	dir := filepath.Join(work, "out", t.Name)
	check(os.MkdirAll(dir, 0o755))
	out := filepath.Join(dir, p.Name)
	os.Remove(out)
	env := append(append(os.Environ(), "GOCACHE="+filepath.Join(work, "gocache"), "MARMOT_MAX_JOBS=2"), t.Env...)

	if *sizeOnly {
		c := exec.Command(t.Build(root, p.Pkg, out, false)[0], t.Build(root, p.Pkg, out, false)[1:]...)
		c.Dir, c.Env = root, env
		if o, err := runGroup(c, 10*time.Minute); err != nil {
			r.Status, r.Error = "build_failed", trunc(fmt.Sprintf("%v: %s", err, o))
			return r
		}
		fi, err := os.Stat(out)
		if err != nil {
			r.Status, r.Error = "build_failed", "no output binary"
			return r
		}
		r.Size, r.SizeStripped = fi.Size(), strippedSize(out)
		r.Status = "ok"
		return r
	}
	// Cold build. Each sample cleans first, so the stdlib is rebuilt too.
	if !*skipCold {
		clean := t.Clean(root, work)
		argv := t.Build(root, p.Pkg, out, true)
		if st, err := timeBuild(argv, clean, *coldRuns, 0, env); err != nil {
			r.Status, r.Error = "build_failed", trunc(err.Error())
			return r
		} else {
			r.BuildCold = st
		}
	}
	// Warm build. One warmup populates the caches.
	argv := t.Build(root, p.Pkg, out, false)
	st, err := timeBuild(argv, "", *warmRuns, 1, env)
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

	var rss []int64
	r.Run, rss, err = timeRuns(func() *exec.Cmd { return exec.Command(out) }, "", *runRuns, 1, os.Environ(), time.Duration(*timeoutSec)*time.Second, true)
	if err != nil {
		r.Status, r.Error = "run_failed", trunc(err.Error())
		return r
	}
	r.RSS = medianInt(rss)
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

// timeRuns runs a command warmup+runs times and summarizes the measured runs. Timing is done
// here, with no external tools: wall time from the clock, CPU time and peak RSS from the child's
// rusage. prepare, if not empty, is a shell command run before every run (not timed). The command
// runs in its own process group with a timeout. rss holds the peak RSS in bytes of each run.
func timeRuns(mk func() *exec.Cmd, prepare string, runs, warmup int, env []string, timeout time.Duration, hold bool) (st *Stat, rss []int64, err error) {
	var wall, cpu []float64
	var release func()
	var heldAt time.Time
	defer func() {
		if release != nil {
			release()
		}
	}()
	for i := 0; i < warmup+runs; i++ {
		// Hold every build slot while timing so no other marmot build starts, but never for
		// longer than about 45 s at a time: release and take them again in between.
		if hold && (release == nil || time.Since(heldAt) > 45*time.Second) {
			if release != nil {
				release()
			}
			release = holdAllSlots()
			heldAt = time.Now()
		}
		if prepare != "" {
			cc := exec.Command("sh", "-c", prepare)
			cc.Dir, cc.Env = root, env
			runGroup(cc, 2*time.Minute)
		}
		c := mk()
		c.Dir, c.Env = root, env
		t0 := time.Now()
		out, err := runGroup(c, timeout)
		d := time.Since(t0)
		if err != nil {
			return nil, nil, fmt.Errorf("%v: %s", err, trunc(string(out)))
		}
		if i < warmup {
			continue
		}
		wall = append(wall, float64(d)/float64(time.Millisecond))
		if ps := c.ProcessState; ps != nil {
			cpu = append(cpu, float64(ps.UserTime()+ps.SystemTime())/float64(time.Millisecond))
			if ru, ok := ps.SysUsage().(*syscall.Rusage); ok {
				// Maxrss is in bytes on darwin and in kilobytes on linux.
				n := int64(ru.Maxrss)
				if runtime.GOOS != "darwin" {
					n *= 1024
				}
				rss = append(rss, n)
			}
		}
	}
	st = summarize(wall)
	st.CPU = median(cpu)
	return st, rss, nil
}

func summarize(v []float64) *Stat {
	s := &Stat{Runs: len(v)}
	if len(v) == 0 {
		return s
	}
	x := append([]float64(nil), v...)
	sort.Float64s(x)
	s.Min, s.Median = x[0], median(x)
	sum := 0.0
	for _, e := range x {
		sum += e
	}
	s.Mean = sum / float64(len(x))
	if len(x) > 1 {
		var sq float64
		for _, e := range x {
			sq += (e - s.Mean) * (e - s.Mean)
		}
		s.Stddev = math.Sqrt(sq / float64(len(x)-1))
	}
	return s
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	x := append([]float64(nil), v...)
	sort.Float64s(x)
	if len(x)%2 == 1 {
		return x[len(x)/2]
	}
	return (x[len(x)/2-1] + x[len(x)/2]) / 2
}

func medianInt(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	x := append([]int64(nil), v...)
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	return x[len(x)/2]
}

func timeBuild(argv []string, clean string, runs, warmup int, env []string) (*Stat, error) {
	st, _, err := timeRuns(func() *exec.Cmd { return exec.Command(argv[0], argv[1:]...) }, clean, runs, warmup, env, 10*time.Minute, false)
	return st, err
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
		t0 := time.Now()
		a, err := runOnce(out)
		check(err)
		el := time.Since(t0)
		b2, _ := runOnce(out)
		if a != b2 {
			check(fmt.Errorf("%s: nondeterministic output", p.Name))
		}
		check(os.WriteFile(filepath.Join(dir, p.Name+".txt"), []byte(a), 0o644))
		fmt.Printf("expected %s (go %.1fs)\n", p.Name, el.Seconds())
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
	if runtime.GOOS == "darwin" {
		m.CPU = sh("sysctl", "-n", "machdep.cpu.brand_string")
		m.MemBytes, _ = strconv.ParseInt(sh("sysctl", "-n", "hw.memsize"), 10, 64)
		m.OS = sh("sw_vers", "-productName") + " " + sh("sw_vers", "-productVersion")
		m.LoadAvg = sh("sysctl", "-n", "vm.loadavg")
	} else {
		m.CPU = procField("/proc/cpuinfo", "model name")
		if kb, err := strconv.ParseInt(strings.Fields(procField("/proc/meminfo", "MemTotal"))[0], 10, 64); err == nil {
			m.MemBytes = kb * 1024
		}
		m.OS = strings.Trim(procField("/etc/os-release", "PRETTY_NAME"), `"`) + " " + sh("uname", "-r")
		if b, err := os.ReadFile("/proc/loadavg"); err == nil {
			m.LoadAvg = strings.TrimSpace(string(b))
		}
	}
	m.GitRev = sh("git", "-C", root, "rev-parse", "--short", "HEAD")
	if snapDir != "" {
		m.GitRev += " (snapshot " + filepath.Base(snapDir) + ")"
	}
	m.Hyperfine = "built-in timer: wall clock, rusage CPU time and Maxrss, no external tools"
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

// procField returns the value of the first "key: value" or "key=value" line in a file.
func procField(path, key string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, key) {
			if i := strings.IndexAny(l, ":="); i >= 0 {
				return strings.TrimSpace(l[i+1:])
			}
		}
	}
	return ""
}

var lockFile *os.File

// lockBenchmarks takes an exclusive lock so only one benchmark run times at a time on this
// checkout, whoever started it. A second runner waits and says so.
func lockBenchmarks() {
	check(os.MkdirAll(filepath.Join(root, ".work"), 0o755))
	f, err := os.OpenFile(filepath.Join(root, ".work", "bench.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	check(err)
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		fmt.Fprintln(os.Stderr, "another benchmark run holds .work/bench.lock; waiting")
		check(syscall.Flock(int(f.Fd()), syscall.LOCK_EX))
	}
	lockFile = f // held until the process exits
}

// loadAvg1 returns the one minute load average, or 0 when it cannot be read.
func loadAvg1() float64 {
	var s string
	if runtime.GOOS == "darwin" {
		s = strings.Trim(sh("sysctl", "-n", "vm.loadavg"), "{} ")
	} else if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		s = string(b)
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

// measureQuiet measures one program and retries (after a pause) when the machine load was above
// -max-load at the start or the end of the measurement. After three tries the row is kept but
// flagged noisy and its timing is dropped by the report.
func measureQuiet(p Program, t Toolchain) Result {
	var r Result
	for try := 0; try < 3; try++ {
		start := loadAvg1()
		r = measure(p, t)
		r.Load, r.LoadEnd = start, loadAvg1()
		r.Rev = benchRev
		if *maxLoad <= 0 || (r.Load <= *maxLoad && r.LoadEnd <= *maxLoad) || r.Run == nil {
			r.Noisy = false
			return r
		}
		fmt.Fprintf(os.Stderr, "   load %.1f/%.1f above %.0f, retrying after a pause\n", r.Load, r.LoadEnd, *maxLoad)
		time.Sleep(20 * time.Second)
	}
	r.Noisy = true
	return r
}

// holdAllSlots takes every machine-wide marmot build slot (the flock files the driver uses, see
// internal/driver/slots.go) so no other marmot build can start, then waits for running backends
// to drain. The returned function releases the slots. If slots are off or the directory is
// missing it does nothing. The bench's own builds never call this.
func holdAllSlots() func() {
	n := 3
	if v, err := strconv.Atoi(os.Getenv("MARMOT_SLOTS")); err == nil && v >= 0 {
		n = v
	}
	dir := os.Getenv("MARMOT_SLOTS_DIR")
	if dir == "" {
		dir = "/tmp/marmot-slots"
	}
	if n == 0 {
		return func() {}
	}
	os.MkdirAll(dir, 0o777)
	os.Chmod(dir, 0o777)
	var files []*os.File
	deadline := time.Now().Add(3 * time.Minute)
	for i := 0; i < n; i++ {
		f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("slot-%d", i)), os.O_RDWR|os.O_CREATE, 0o666)
		if err != nil {
			break
		}
		for syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		files = append(files, f)
	}
	// Let running backends and compilers finish: wait until none of them runs for 3 s in a row.
	quiet := 0
	for t := time.Now(); quiet < 3 && time.Since(t) < 40*time.Second; {
		if exec.Command("pgrep", "-f", "marmot-backend").Run() != nil {
			quiet++
		} else {
			quiet = 0
		}
		time.Sleep(time.Second)
	}
	return func() {
		for _, f := range files {
			f.Close() // closing drops the flock
		}
	}
}

// benchRev is the git revision and snapshot name recorded on every row of this run.
var benchRev string

// pruned reports whether a toolchain adds nothing for a program: the spec toolchain only changes
// programs that import the standard library, and the loop flags only matter for numeric code.
func pruned(p Program, t Toolchain) bool {
	switch t.Name {
	case "marmot-release-spec":
		return p.Tier == "nostd"
	case "marmot-release-loops":
		if p.Tier != "std" {
			return false
		}
		data, err := os.ReadFile(filepath.Join(root, "bench", "classes.json"))
		if err != nil {
			return false
		}
		var cl struct {
			Programs map[string]struct{ Class string } `json:"programs"`
		}
		if json.Unmarshal(data, &cl) != nil {
			return false
		}
		return cl.Programs[p.Name].Class != "A"
	}
	return false
}
