// Command buildbench measures build time: go build against marmot dev and marmot -release on a
// fixed set of programs, in five scenarios.
//
//	no-change   rebuild with nothing changed
//	edit-main   rebuild after a one line edit in the main package
//	edit-dep    rebuild after a one line edit in the most imported non-standard package
//	cold-std    first build of the program with the standard library already built
//	first-ever  first build on a machine with nothing cached
//
// Each sample runs in its own process group with all cores and no job caps (the marmot slot and
// job variables are off for the measured command only). While a sample runs, every marmot build
// slot is held so no other marmot build competes. Wall time, CPU time (user plus system) and,
// for marmot, the phase breakdown from `marmot build -v` are recorded. Results go to
// bench/results/DATE-build-LABEL.json and bench/BUILD.md; bench/BUILD-table.md is the short table.
//
//	go run ./bench/cmd/buildbench [-programs hello,fmt,cmd-marmot] [-runs 5] [-label x]
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

var (
	runs      = flag.Int("runs", 5, "samples per scenario (first-ever uses at most 3)")
	label     = flag.String("label", "quiet", "label for the results file")
	programs  = flag.String("programs", "", "comma separated program names (default: all)")
	tools     = flag.String("toolchains", "go,marmot-dev,marmot-release", "toolchains to measure")
	scenarios = flag.String("scenarios", "no-change,edit-main,edit-dep,cold-std,first-ever", "scenarios to measure")
	listOnly  = flag.Bool("list", false, "list programs and exit")
	noHold    = flag.Bool("no-hold", false, "do not hold the build slots while timing (not for published numbers)")
	timeoutS  = flag.Int("timeout", 900, "kill one build after this many seconds")
	root      string
	work      string
)

// Program is one thing to build.
type Program struct {
	Name string
	// SrcDir is copied to the scratch area; Dir is the directory (relative to the copy) to build
	// in; Target is the argument of the build command ("." or a package path or a file).
	SrcDir string
	Dir    string
	Target string
	// MainFile is the file (relative to the copy) edited in the edit-main scenario.
	MainFile string
}

// Sample is one timed build.
type Sample struct {
	WallMS float64            `json:"wall_ms"`
	CPUMS  float64            `json:"cpu_ms"`
	Phases map[string]float64 `json:"phases_ms,omitempty"`
}

// Cell is the summary of the samples of one program, toolchain and scenario.
type Cell struct {
	Program   string             `json:"program"`
	Toolchain string             `json:"toolchain"`
	Scenario  string             `json:"scenario"`
	Status    string             `json:"status"` // ok, failed, n/a
	Error     string             `json:"error,omitempty"`
	WallMS    float64            `json:"wall_ms_median,omitempty"`
	CPUMS     float64            `json:"cpu_ms_median,omitempty"`
	MinMS     float64            `json:"wall_ms_min,omitempty"`
	Phases    map[string]float64 `json:"phases_ms_median,omitempty"`
	Samples   []Sample           `json:"samples,omitempty"`
	Edited    string             `json:"edited_file,omitempty"`
}

// File is the results document.
type File struct {
	Meta  map[string]any `json:"meta"`
	Cells []Cell         `json:"cells"`
}

func main() {
	// Plain compiler output whoever runs the harness (marmot switches to
	// JSON under coding agents).
	os.Setenv("MARMOT_OUTPUT", "plain")
	flag.Parse()
	root = findRoot()
	work = filepath.Join(root, ".work", "buildbench")
	progs := discover()
	if *programs != "" {
		want := map[string]bool{}
		for _, n := range strings.Split(*programs, ",") {
			want[strings.TrimSpace(n)] = true
		}
		var f []Program
		for _, p := range progs {
			if want[p.Name] {
				f = append(f, p)
			}
		}
		progs = f
	}
	if *listOnly {
		for _, p := range progs {
			fmt.Println(p.Name, p.SrcDir, p.Dir, p.Target)
		}
		return
	}
	lock()
	os.RemoveAll(work)
	check(os.MkdirAll(work, 0o755))
	defer os.RemoveAll(work)
	snap := makeSnapshot()

	meta := map[string]any{
		"date": time.Now().Format("2006-01-02"), "label": *label,
		"os": runtime.GOOS, "arch": runtime.GOARCH, "ncpu": runtime.NumCPU(),
		"go": sh("go", "version"), "git_rev": sh("git", "-C", root, "rev-parse", "--short", "HEAD"),
		"runs": *runs, "load_avg_at_start": loadAvg1(),
		"notes": "all cores, no slot or job caps for the measured command, build slots held while timing",
	}
	if runtime.GOOS == "darwin" {
		meta["cpu"] = sh("sysctl", "-n", "machdep.cpu.brand_string")
	}
	out := File{Meta: meta}
	path := filepath.Join(root, "bench", "results", meta["date"].(string)+"-build-"+*label+".json")
	check(os.MkdirAll(filepath.Dir(path), 0o755))

	tcs := strings.Split(*tools, ",")
	scs := strings.Split(*scenarios, ",")
	// Preheat caches that contain only the standard library, once per toolchain.
	pre := map[string]string{}
	for _, tc := range tcs {
		pre[tc] = preheat(tc, snap)
	}
	for _, p := range progs {
		src := copySource(p)
		for _, tc := range tcs {
			for _, sc := range scs {
				fmt.Fprintf(os.Stderr, "== %s / %s / %s\n", p.Name, tc, sc)
				c := measure(p, src, tc, sc, snap, pre[tc])
				fmt.Fprintf(os.Stderr, "   %s %.0fms (cpu %.0fms) %s\n", c.Status, c.WallMS, c.CPUMS, c.Error)
				out.Cells = append(out.Cells, c)
				data, _ := json.MarshalIndent(out, "", "  ")
				os.WriteFile(path, append(data, '\n'), 0o644)
			}
		}
		os.RemoveAll(src)
	}
	writeReports(out)
	fmt.Fprintln(os.Stderr, "wrote", path)
}

// ---------------------------------------------------------------------------------------------
// Programs

func discover() []Program {
	ps := []Program{
		{Name: "hello", SrcDir: ".", Dir: ".", Target: "./bench/programs/hello", MainFile: "bench/programs/hello/main.go"},
		{Name: "fmt", SrcDir: ".", Dir: ".", Target: "./bench/programs/hellostd", MainFile: "bench/programs/hellostd/main.go"},
		{Name: "cmd-marmot", SrcDir: ".", Dir: ".", Target: "./cmd/marmot", MainFile: "cmd/marmot/main.go"},
	}
	worlds, _ := filepath.Glob(filepath.Join(root, "tests", "world", "*", "go.mod"))
	for _, w := range worlds {
		d := filepath.Dir(w)
		n := filepath.Base(d)
		ps = append(ps, Program{Name: "world-" + n, SrcDir: filepath.Join("tests", "world", n), Dir: ".", Target: ".", MainFile: "main.go"})
	}
	apps, _ := filepath.Glob(filepath.Join(root, "tests", "run", "apps", "*.go"))
	for _, a := range apps {
		b := filepath.Base(a)
		ps = append(ps, Program{Name: "app-" + strings.TrimSuffix(b, ".go"), SrcDir: filepath.Join("tests", "run", "apps"), Dir: ".", Target: b, MainFile: b})
	}
	return ps
}

// copySource copies the program's source tree to the scratch area (cloned where the file system
// supports it) so edits never touch the checkout.
func copySource(p Program) string {
	dst := filepath.Join(work, "src-"+p.Name)
	os.RemoveAll(dst)
	check(os.MkdirAll(dst, 0o755))
	src := filepath.Join(root, p.SrcDir)
	if p.SrcDir == "." {
		// The repository: skip build output, scratch, history and test corpora.
		args := []string{"-a", "--exclude=.git", "--exclude=.work", "--exclude=build", "--exclude=docs", "--exclude=tests", "--exclude=backend", "--exclude=runtime", src + "/", dst + "/"}
		if out, err := exec.Command("rsync", args...).CombinedOutput(); err != nil {
			check(fmt.Errorf("copy: %v %s", err, out))
		}
	} else if strings.HasSuffix(p.Target, ".go") {
		// A single file program: copy just that file plus a module file for go build.
		data, err := os.ReadFile(filepath.Join(src, p.Target))
		check(err)
		check(os.WriteFile(filepath.Join(dst, p.Target), data, 0o644))
		check(os.WriteFile(filepath.Join(dst, "go.mod"), []byte("module app\n\ngo 1.27\n"), 0o644))
	} else {
		if out, err := exec.Command("rsync", "-a", src+"/", dst+"/").CombinedOutput(); err != nil {
			check(fmt.Errorf("copy: %v %s", err, out))
		}
	}
	return dst
}

// ---------------------------------------------------------------------------------------------
// Toolchains and caches

// makeSnapshot copies what marmot needs (installed binaries, libraries and the Go overlay) to the
// scratch area, without any object cache.
func makeSnapshot() string {
	snap := filepath.Join(work, "snap")
	for _, d := range []string{"build/bin", "build/lib", "lib"} {
		dst := filepath.Join(snap, d)
		check(os.MkdirAll(filepath.Dir(dst), 0o755))
		if out, err := exec.Command("cp", "-R", filepath.Join(root, d), dst).CombinedOutput(); err != nil {
			check(fmt.Errorf("snapshot: %v %s", err, out))
		}
	}
	return snap
}

// env describes the cache state of one build: a Go cache directory and a marmot root.
type env struct {
	gocache string
	mroot   string
}

func (e env) environ(tc string) []string {
	v := os.Environ()
	v = append(v, "GOCACHE="+e.gocache, "GOFLAGS=-mod=vendor")
	if strings.HasPrefix(tc, "marmot") {
		// No job caps and no slots for the measured command only.
		v = append(v, "MARMOT_ROOT="+e.mroot, "MARMOT_SLOTS=0", "MARMOT_CACHE_MAX_MB=4096")
		v = append(v, tier1Off()...)
		v = filterOut(v, "MARMOT_MAX_JOBS", "MARMOT_BACKEND_FLAGS", "MARMOT_SPEC")
	}
	return v
}

func filterOut(env []string, names ...string) []string {
	var out []string
outer:
	for _, e := range env {
		for _, n := range names {
			if strings.HasPrefix(e, n+"=") {
				continue outer
			}
		}
		out = append(out, e)
	}
	return out
}

func buildArgv(tc, mroot, target, outFile string, verbose bool) []string {
	switch tc {
	case "go":
		return []string{"go", "build", "-o", outFile, target}
	case "marmot-dev", "marmot-release":
		a := []string{filepath.Join(mroot, "build", "bin", "marmot"), "build"}
		if tc == "marmot-release" && hasReleaseFlag(mroot) {
			a = append(a, "-release")
		}
		if verbose {
			a = append(a, "-v")
		}
		return append(a, "-o", outFile, target)
	}
	check(fmt.Errorf("unknown toolchain %s", tc))
	return nil
}

func hasReleaseFlag(mroot string) bool {
	out, _ := exec.Command(filepath.Join(mroot, "build", "bin", "marmot"), "build", "-h").CombinedOutput()
	return bytes.Contains(out, []byte("-release"))
}

// newEnv makes a fresh cache state: empty, or a clone of a preheated one.
func newEnv(name string, tc, snap, preheated string) env {
	e := env{gocache: filepath.Join(work, "gocache-"+name), mroot: filepath.Join(work, "mroot-"+name)}
	os.RemoveAll(e.gocache)
	os.RemoveAll(e.mroot)
	if strings.HasPrefix(tc, "marmot") {
		cloneDir(snap, e.mroot) // binaries and overlay, no cache
		if preheated != "" {
			cloneDir(filepath.Join(preheated, "build", "cache"), filepath.Join(e.mroot, "build", "cache"))
			cloneGoroot(preheated, e.mroot)
		}
	} else {
		check(os.MkdirAll(e.gocache, 0o755))
		if preheated != "" {
			os.RemoveAll(e.gocache)
			cloneDir(preheated, e.gocache)
		}
	}
	return e
}

func (e env) remove() { os.RemoveAll(e.gocache); os.RemoveAll(e.mroot) }

// cloneDir copies a directory, using copy on write clones where the file system has them.
func cloneDir(src, dst string) {
	if _, err := os.Stat(src); err != nil {
		return
	}
	check(os.MkdirAll(filepath.Dir(dst), 0o755))
	args := []string{"-R", src, dst}
	if runtime.GOOS == "darwin" {
		args = []string{"-cR", src, dst}
	} else {
		args = []string{"-R", "--reflink=auto", src, dst}
	}
	if out, err := exec.Command("cp", args...).CombinedOutput(); err != nil {
		check(fmt.Errorf("clone %s: %v %s", src, err, out))
	}
}

// cloneGoroot copies the merged GOROOT directories (build/goroot-*) the driver extracted.
func cloneGoroot(fromRoot, toRoot string) {
	m, _ := filepath.Glob(filepath.Join(fromRoot, "build", "goroot-*"))
	for _, g := range m {
		cloneDir(g, filepath.Join(toRoot, "build", filepath.Base(g)))
	}
}

// preheat builds a small fmt program with a toolchain from an empty cache, so the cache holds
// the standard library only, and returns where that state lives (a Go cache directory, or a
// marmot root). It is cloned for every cold-std sample.
func preheat(tc, snap string) string {
	e := newEnv("pre-"+tc, tc, snap, "")
	src := copySource(Program{Name: "preheat", SrcDir: ".", Target: "./bench/programs/hellostd"})
	defer os.RemoveAll(src)
	argv := buildArgv(tc, e.mroot, "./bench/programs/hellostd", filepath.Join(work, "preheat.out"), false)
	if _, _, err := runTimed(argv, src, e.environ(tc)); err != nil {
		fmt.Fprintln(os.Stderr, "preheat", tc, "failed:", err)
		return ""
	}
	if strings.HasPrefix(tc, "marmot") {
		return e.mroot
	}
	return e.gocache
}

// ---------------------------------------------------------------------------------------------
// Measuring

func measure(p Program, src, tc, sc, snap, pre string) (c Cell) {
	c = Cell{Program: p.Name, Toolchain: tc, Scenario: sc}
	n := *runs
	if sc == "first-ever" && n > 3 {
		n = 3
	}
	dir := filepath.Join(src, p.Dir)
	outFile := filepath.Join(work, "out-"+p.Name)
	name := p.Name + "-" + tc + "-" + sc
	build := func(e env) (Sample, error) {
		argv := buildArgv(tc, e.mroot, p.Target, outFile, strings.HasPrefix(tc, "marmot"))
		s, stderr, err := runTimed(argv, dir, e.environ(tc))
		if strings.HasPrefix(tc, "marmot") {
			s.Phases = parsePhases(stderr)
		}
		return s, err
	}
	var samples []Sample
	fail := func(err error) Cell {
		c.Status, c.Error = "failed", trunc(err.Error())
		return c
	}
	switch sc {
	case "no-change", "edit-main", "edit-dep":
		e := newEnv(name, tc, snap, "")
		defer e.remove()
		if _, err := build(e); err != nil { // warm build, not timed
			return fail(err)
		}
		editFile := filepath.Join(src, p.MainFile)
		if sc == "edit-dep" {
			f := mostImported(dir, p)
			if f == "" {
				c.Status = "n/a"
				c.Error = "no non-standard package other than main"
				return c
			}
			editFile = f
		}
		for i := 0; i < n; i++ {
			if sc != "no-change" {
				if err := appendLine(editFile, fmt.Sprintf("var _ = %d", time.Now().UnixNano()%1000000007)); err != nil {
					return fail(err)
				}
				c.Edited = strings.TrimPrefix(editFile, src+"/")
			}
			s, err := build(e)
			if err != nil {
				return fail(err)
			}
			samples = append(samples, s)
		}
	case "cold-std", "first-ever":
		for i := 0; i < n; i++ {
			preh := pre
			if sc == "first-ever" {
				preh = ""
			}
			if sc == "cold-std" && pre == "" {
				c.Status, c.Error = "failed", "no preheated cache"
				return c
			}
			e := newEnv(name, tc, snap, preh)
			s, err := build(e)
			e.remove()
			if err != nil {
				return fail(err)
			}
			samples = append(samples, s)
		}
	default:
		c.Status, c.Error = "failed", "unknown scenario"
		return c
	}
	c.Status = "ok"
	c.Samples = samples
	var wall, cpu []float64
	for _, s := range samples {
		wall = append(wall, s.WallMS)
		cpu = append(cpu, s.CPUMS)
	}
	c.WallMS, c.CPUMS = median(wall), median(cpu)
	sort.Float64s(wall)
	c.MinMS = wall[0]
	c.Phases = medianPhases(samples)
	return c
}

// runTimed runs one build in its own process group with all marmot slots held, and returns its
// wall and CPU time and its stderr.
func runTimed(argv []string, dir string, env []string) (Sample, string, error) {
	var release func()
	if !*noHold {
		release = holdAllSlots()
		defer release()
	}
	c := exec.Command(argv[0], argv[1:]...)
	c.Dir, c.Env = dir, env
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var so, se bytes.Buffer
	c.Stdout, c.Stderr = &so, &se
	t0 := time.Now()
	if err := c.Start(); err != nil {
		return Sample{}, "", err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutS)*time.Second)
	defer cancel()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		err = fmt.Errorf("timeout after %ds", *timeoutS)
		syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		<-done
	}
	wall := time.Since(t0)
	syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	s := Sample{WallMS: float64(wall) / float64(time.Millisecond)}
	if ps := c.ProcessState; ps != nil {
		s.CPUMS = float64(ps.UserTime()+ps.SystemTime()) / float64(time.Millisecond)
	}
	if err != nil {
		return s, se.String(), fmt.Errorf("%v: %s", err, trunc(se.String()+so.String()))
	}
	return s, se.String(), nil
}

// holdAllSlots takes every machine-wide marmot build slot (flock files) so no other marmot build
// can start, then waits until no backend has run for 3 seconds. The result releases them.
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
	var files []*os.File
	deadline := time.Now().Add(3 * time.Minute)
	for i := 0; i < n; i++ {
		f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("slot-%d", i)), os.O_RDWR|os.O_CREATE, 0o666)
		if err != nil {
			break
		}
		for syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil && time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
		}
		files = append(files, f)
	}
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
			f.Close()
		}
	}
}

var phaseRe = regexp.MustCompile(`^marmot:\s+(\d+)ms (.*)$`)

// parsePhases turns the timestamps in `marmot build -v` output into phase durations: front end
// (until all packages are loaded), codegen (until the MLIR modules exist), backend (until the
// objects are compiled) and link (the rest).
func parsePhases(stderr string) map[string]float64 {
	var stamps []struct {
		t   float64
		msg string
	}
	for _, l := range strings.Split(stderr, "\n") {
		if m := phaseRe.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			v, _ := strconv.ParseFloat(m[1], 64)
			stamps = append(stamps, struct {
				t   float64
				msg string
			}{v, m[2]})
		}
	}
	if len(stamps) == 0 {
		return nil
	}
	at := func(prefix string) (float64, bool) {
		for _, s := range stamps {
			if strings.HasPrefix(s.msg, prefix) {
				return s.t, true
			}
		}
		return 0, false
	}
	start := 0.0
	if t, ok := at("got build slot"); ok {
		start = t
	}
	loaded, ok1 := at("loaded")
	gen, ok2 := at("generated")
	comp, ok3 := at("compiled")
	end := stamps[len(stamps)-1].t
	ph := map[string]float64{}
	if ok1 && ok2 && ok3 {
		ph["frontend"] = loaded - start
		ph["codegen"] = gen - loaded
		ph["backend"] = comp - gen
		ph["link"] = end - comp
	}
	return ph
}

func medianPhases(ss []Sample) map[string]float64 {
	keys := map[string][]float64{}
	for _, s := range ss {
		for k, v := range s.Phases {
			keys[k] = append(keys[k], v)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	out := map[string]float64{}
	for k, v := range keys {
		out[k] = median(v)
	}
	return out
}

// mostImported returns a file in the non-standard package (other than main) that is imported by
// the most packages of the program, or "" if there is none.
func mostImported(dir string, p Program) string {
	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}|{{.Dir}}|{{.Standard}}|{{.Name}}|{{join .Imports \" \"}}|{{join .GoFiles \" \"}}", p.Target)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=vendor")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	type pkg struct {
		dir, files string
		std        bool
		name       string
	}
	pk := map[string]pkg{}
	count := map[string]int{}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Split(l, "|")
		if len(f) < 6 {
			continue
		}
		pk[f[0]] = pkg{dir: f[1], std: f[2] == "true", name: f[3], files: f[5]}
		for _, imp := range strings.Fields(f[4]) {
			count[imp]++
		}
	}
	best, bestN := "", 0
	for ip, n := range count {
		q, ok := pk[ip]
		if !ok || q.std || q.name == "main" || strings.HasPrefix(ip, "internal/") || q.files == "" {
			continue
		}
		if n > bestN || (n == bestN && ip < best) {
			best, bestN = ip, n
		}
	}
	if best == "" {
		return ""
	}
	q := pk[best]
	f := strings.Fields(q.files)[0]
	return filepath.Join(q.dir, f)
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n" + line + "\n")
	return err
}

// ---------------------------------------------------------------------------------------------
// Reports

func writeReports(f File) {
	by := map[string]Cell{}
	var progs []string
	seen := map[string]bool{}
	for _, c := range f.Cells {
		by[c.Program+"|"+c.Toolchain+"|"+c.Scenario] = c
		if !seen[c.Program] {
			seen[c.Program] = true
			progs = append(progs, c.Program)
		}
	}
	scs := strings.Split(*scenarios, ",")
	tcs := strings.Split(*tools, ",")
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("# Build time\n\nGenerated by `go run ./bench/cmd/buildbench`. Date %v, %v, %v cores, `%v`, load average at start %v.\n\n", f.Meta["date"], f.Meta["cpu"], f.Meta["ncpu"], f.Meta["go"], f.Meta["load_avg_at_start"])
	w("All cores and no job caps for the measured command. Build slots held while timing. Median of %v samples (first-ever: at most 3). Wall time, with CPU time in parentheses. Ratio is marmot over go build, lower is better, 1.00 is parity.\n\n", f.Meta["runs"])
	cell := func(p, tc, sc string) string {
		c, ok := by[p+"|"+tc+"|"+sc]
		if !ok {
			return ""
		}
		switch c.Status {
		case "failed":
			return "FAIL"
		case "n/a":
			return "n/a"
		}
		return fmt.Sprintf("%s (%s)", ms(c.WallMS), ms(c.CPUMS))
	}
	ratio := func(p, tc, sc string) (float64, bool) {
		a, ok1 := by[p+"|"+tc+"|"+sc]
		g, ok2 := by[p+"|go|"+sc]
		if !ok1 || !ok2 || a.Status != "ok" || g.Status != "ok" || g.WallMS <= 0 {
			return 0, false
		}
		return a.WallMS / g.WallMS, true
	}
	w("## Geometric mean of the ratio to go build\n\n| Scenario |")
	for _, tc := range tcs {
		if tc != "go" {
			w(" %s |", tc)
		}
	}
	w("\n|---|")
	for _, tc := range tcs {
		if tc != "go" {
			w("---|")
		}
	}
	w("\n")
	for _, sc := range scs {
		w("| %s |", sc)
		for _, tc := range tcs {
			if tc == "go" {
				continue
			}
			sum, n := 0.0, 0
			for _, p := range progs {
				if r, ok := ratio(p, tc, sc); ok {
					sum += math.Log(r)
					n++
				}
			}
			if n == 0 {
				w(" n/a |")
			} else {
				w(" %.2fx (%d) |", math.Exp(sum/float64(n)), n)
			}
		}
		w("\n")
	}
	w("\n")
	for _, sc := range scs {
		w("## %s\n\n| Program |", sc)
		for _, tc := range tcs {
			w(" %s |", tc)
			if tc != "go" {
				w(" ratio |")
			}
		}
		w("\n|---|")
		for _, tc := range tcs {
			w("---|")
			if tc != "go" {
				w("---|")
			}
		}
		w("\n")
		for _, p := range progs {
			w("| %s |", p)
			for _, tc := range tcs {
				w(" %s |", cell(p, tc, sc))
				if tc != "go" {
					if r, ok := ratio(p, tc, sc); ok {
						w(" %.2fx |", r)
					} else {
						w(" |")
					}
				}
			}
			w("\n")
		}
		w("\n")
	}
	w("## Phases of a marmot build (median ms)\n\n| Program | Toolchain | Scenario | frontend | codegen | backend | link |\n|---|---|---|---|---|---|---|\n")
	for _, p := range progs {
		for _, tc := range tcs {
			for _, sc := range scs {
				c, ok := by[p+"|"+tc+"|"+sc]
				if !ok || len(c.Phases) == 0 {
					continue
				}
				w("| %s | %s | %s | %.0f | %.0f | %.0f | %.0f |\n", p, tc, sc, c.Phases["frontend"], c.Phases["codegen"], c.Phases["backend"], c.Phases["link"])
			}
		}
	}
	check(os.WriteFile(filepath.Join(root, "bench", "BUILD.md"), []byte(b.String()), 0o644))

	// Short table: hello, fmt and cmd-marmot, wall time and ratio.
	var t strings.Builder
	fmt.Fprintf(&t, "| Scenario | Program | go build | marmot dev | ratio | marmot release | ratio |\n|---|---|---|---|---|---|---|\n")
	for _, sc := range scs {
		for _, p := range []string{"hello", "fmt", "cmd-marmot"} {
			if _, ok := by[p+"|go|"+sc]; !ok {
				continue
			}
			rc := func(tc string) string {
				if r, ok := ratio(p, tc, sc); ok {
					return fmt.Sprintf("%.2fx", r)
				}
				return "-"
			}
			wallOf := func(tc string) string {
				c, ok := by[p+"|"+tc+"|"+sc]
				if !ok || c.Status != "ok" {
					return "-"
				}
				return ms(c.WallMS)
			}
			fmt.Fprintf(&t, "| %s | %s | %s | %s | %s | %s | %s |\n", sc, p, wallOf("go"), wallOf("marmot-dev"), rc("marmot-dev"), wallOf("marmot-release"), rc("marmot-release"))
		}
	}
	check(os.WriteFile(filepath.Join(root, "bench", "BUILD-table.md"), []byte(t.String()), 0o644))
}

func ms(v float64) string {
	switch {
	case v >= 10000:
		return fmt.Sprintf("%.1f s", v/1000)
	case v >= 1000:
		return fmt.Sprintf("%.2f s", v/1000)
	}
	return fmt.Sprintf("%.0f ms", v)
}

// ---------------------------------------------------------------------------------------------
// Helpers

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

func trunc(s string) string {
	if len(s) > 800 {
		return s[:800] + "..."
	}
	return s
}

func loadAvg1() float64 {
	f := strings.Fields(strings.Trim(sh("sysctl", "-n", "vm.loadavg"), "{} "))
	if len(f) == 0 {
		if b, err := os.ReadFile("/proc/loadavg"); err == nil {
			f = strings.Fields(string(b))
		}
	}
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

func sh(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var lockFile *os.File

// lock takes the same exclusive lock as the speed runner so only one timing run is active.
func lock() {
	check(os.MkdirAll(filepath.Join(root, ".work"), 0o755))
	f, err := os.OpenFile(filepath.Join(root, ".work", "bench.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	check(err)
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		fmt.Fprintln(os.Stderr, "another benchmark run is active; waiting")
		check(syscall.Flock(int(f.Fd()), syscall.LOCK_EX))
	}
	lockFile = f
}

func findRoot() string {
	d, _ := os.Getwd()
	for ; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	check(fmt.Errorf("cannot find the repository root"))
	return ""
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "buildbench:", err)
		os.Exit(1)
	}
}

// tier1Off keeps background tier 1 out of the runs (it would compete for
// cores with the builds being measured) unless MARMOT_TIER1 is set.
func tier1Off() []string {
	if os.Getenv("MARMOT_TIER1") != "" {
		return nil
	}
	return []string{"MARMOT_TIER1=0"}
}
