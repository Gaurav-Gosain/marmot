// Command conform runs the marmot conformance suite.
//
// Each test is a single-file package main, tests/<tree>/<area>/NAME.go, with goldens next to it:
//
//	NAME.out   normalized stdout
//	NAME.err   normalized stderr (optional, absent when empty)
//	NAME.exit  exit status (optional, absent when 0)
//
// Normalization of stderr keeps everything up to and including the first line that starts with
// "panic: " or "fatal error: ". Stack traces and goroutine dumps are dropped because marmot only
// prints the message. Goldens are produced by the stock Go toolchain with -update.
//
// Run from the repo root: go run ./tests/conform
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type result struct {
	Name     string  `json:"name"`
	Dir      string  `json:"dir"`
	Status   string  `json:"status"` // pass, fail
	Reason   string  `json:"reason,omitempty"`
	Detail   string  `json:"detail,omitempty"`
	Known    bool    `json:"known_failure,omitempty"`
	Seconds  float64 `json:"seconds"`
	Unexpect bool    `json:"unexpected_pass,omitempty"`
}

type outcome struct {
	stdout, stderr string
	exit           int
	timedOut       bool
}

var (
	flRun     = flag.String("run", "", "regexp: only run tests whose path matches")
	flDir     = flag.String("dir", "", "only run tests under this directory (relative to tests/, e.g. run/core)")
	flJ       = flag.Int("j", 4, "parallel jobs (at most 4)")
	flUpdate  = flag.Bool("update", false, "regenerate goldens with the stock Go toolchain")
	flV       = flag.Bool("v", false, "print every test, with diff detail for failures")
	flJSON    = flag.String("json", "", "write JSON results to this file (default tests/results.json)")
	flO       = flag.String("O", "", "optimization level passed to marmot build -O")
	flKeep    = flag.Bool("keep", false, "keep the per-test work directory")
	flTimeout = flag.Duration("timeout", 20*time.Second, "run timeout per test")
	flBuildTO = flag.Duration("build-timeout", 300*time.Second, "compile timeout per test")
	flNoWrite = flag.Bool("nowrite", false, "do not write results.json and RESULTS.md")
	flMatrix  = flag.Bool("matrix", false, "run every program at -O0, -O2 and -Oz (plus any -bflags variations); results are named NAME@CONFIG")
	flBFlags  = flag.String("bflags", "", "with -matrix: extra configs as label=FLAGS pairs separated by ';', run at -O2 with MARMOT_BACKEND_FLAGS=FLAGS")
	flModes   = flag.String("modes", "", "comma list of build modes to run every program in: dev, release, gcstress, poison, stress, lifetime, evalorder, O0, O1, O2, O3, Os, Oz; results are named NAME@MODE")
	flLive    = flag.Bool("live", false, "run against the live tree instead of a snapshot (other workers may be mid-edit)")
)

func main() {
	// Plain compiler output whoever runs the harness (marmot switches to
	// JSON under coding agents).
	os.Setenv("MARMOT_OUTPUT", "plain")
	flag.Parse()
	root, err := repoRoot()
	check(err)
	// Keep Go and marmot build caches inside the project so they can be cleaned together.
	if os.Getenv("GOCACHE") == "" {
		os.Setenv("GOCACHE", filepath.Join(root, ".work", "gocache"))
	}
	testsDir := filepath.Join(root, "tests")
	check(os.MkdirAll(filepath.Join(root, ".work"), 0o755))
	// Each run gets its own directory so concurrent runs do not collide.
	work, err := os.MkdirTemp(filepath.Join(root, ".work"), "conform-")
	check(err)
	var snapDir string
	cleanup := func() {
		if !*flKeep {
			os.RemoveAll(work)
			if snapDir != "" {
				os.RemoveAll(snapDir)
			}
		}
	}
	defer cleanup()
	// Clean up the work directory and snapshot when killed by a signal as well.
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigc
		cleanup()
		os.Exit(130)
	}()

	var filter *regexp.Regexp
	if *flRun != "" {
		filter, err = regexp.Compile(*flRun)
		check(err)
	}
	tests := discover(testsDir, *flDir, filter)
	if len(tests) == 0 {
		fmt.Println("no tests found")
		return
	}

	if *flUpdate {
		update(root, testsDir, work, tests)
		return
	}

	gitSHA := gitHead(root)
	snapRoot := root
	if !*flLive {
		snapRoot, err = snapshot(root, gitSHA)
		check(err)
		snapDir = snapRoot
	}
	marmot := filepath.Join(snapRoot, "build", "bin", "marmot")
	if _, err := os.Stat(marmot); err != nil {
		fmt.Fprintf(os.Stderr, "conform: %s not found (run make frontend); every test will fail\n", marmot)
	}
	known := loadKnown(filepath.Join(testsDir, "known_failures.txt"))

	configs := buildConfigs()
	type job struct {
		test string
		cfg  config
	}
	var jobs []job
	for _, t := range tests {
		for _, c := range configs {
			jobs = append(jobs, job{t, c})
		}
	}
	results := make([]result, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, min(max(*flJ, 1), 4))
	for i, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = runOne(snapRoot, testsDir, work, marmot, j.test, j.cfg)
			results[i].Known = known[j.test] || (j.cfg.label != "" && known[j.test+"@"+j.cfg.label])
		}()
	}
	wg.Wait()

	unexpected := 0
	for i := range results {
		r := &results[i]
		if r.Status == "fail" && !r.Known {
			unexpected++
		}
		if r.Status == "pass" && r.Known {
			r.Unexpect = true
		}
		switch {
		case r.Status == "fail" && (!r.Known || *flV):
			tag := "FAIL"
			if r.Known {
				tag = "known"
			}
			fmt.Printf("%s %s: %s\n", tag, r.Name, r.Reason)
			if *flV && r.Detail != "" {
				fmt.Println(indent(r.Detail))
			}
		case r.Status == "pass" && r.Unexpect:
			fmt.Printf("note %s passes but is listed in known_failures.txt\n", r.Name)
		case *flV:
			fmt.Printf("ok   %s\n", r.Name)
		}
	}
	printSummary(results)

	if !*flNoWrite && *flRun == "" && *flDir == "" {
		jsonPath := *flJSON
		if jsonPath == "" {
			jsonPath = filepath.Join(testsDir, "results.json")
		}
		writeJSON(jsonPath, results, gitSHA)
		check(os.WriteFile(filepath.Join(testsDir, "RESULTS.md"), []byte(resultsMD(results, gitSHA)), 0o644))
	} else if *flJSON != "" {
		writeJSON(*flJSON, results, gitSHA)
	}
	if unexpected > 0 {
		cleanup()
		os.Exit(1)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "conform:", err)
		os.Exit(2)
	}
}

func repoRoot() (string, error) {
	if r := os.Getenv("MARMOT_ROOT"); r != "" {
		return r, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "cmd", "marmot")); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("cannot find the repository root (a directory with cmd/marmot) above %s", dir)
		}
	}
}

// discover returns test names relative to tests/, without the .go suffix, sorted.
func discover(testsDir, dir string, filter *regexp.Regexp) []string {
	var out []string
	for _, tree := range []string{"run", "goroot"} {
		base := filepath.Join(testsDir, tree)
		filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
				return nil
			}
			rel, _ := filepath.Rel(testsDir, p)
			rel = strings.TrimSuffix(filepath.ToSlash(rel), ".go")
			if dir != "" && !inDirs(rel, dir) {
				return nil
			}
			if filter != nil && !filter.MatchString(rel) {
				return nil
			}
			out = append(out, rel)
			return nil
		})
	}
	// world tier: tests/world/NAME is a module directory with main.go and golden.* files.
	if ents, err := os.ReadDir(filepath.Join(testsDir, "world")); err == nil {
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(testsDir, "world", e.Name(), "main.go")); err != nil {
				continue
			}
			rel := "world/" + e.Name()
			if dir != "" && !inDirs(rel, dir) && !inDirs(rel+"/x", dir) {
				continue
			}
			if filter != nil && !filter.MatchString(rel) {
				continue
			}
			if !modulesAvailable(filepath.Join(testsDir, rel)) {
				fmt.Printf("skip %s: its modules are not in the module cache and cannot be downloaded (offline?)\n", rel)
				continue
			}
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// goldenBase returns the path prefix of the golden files and the build target for a test.
// Single file tests keep NAME.out next to NAME.go. World tests keep golden.out in the module dir.
func goldenBase(testsDir, name string) (golden string, srcDir string, target string) {
	if strings.HasPrefix(name, "world/") {
		d := filepath.Join(testsDir, name)
		return filepath.Join(d, "golden"), d, "."
	}
	return filepath.Join(testsDir, name), testsDir, filepath.Join(testsDir, name+".go")
}

func loadKnown(path string) map[string]bool {
	m := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSuffix(strings.TrimSpace(line), ".go")
		if line != "" {
			m[line] = true
		}
	}
	return m
}

// execute runs a command with a timeout and captures output.
func execute(timeout time.Duration, dir string, env []string, name string, args ...string) (outcome, error) {
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
	o := outcome{stdout: so.String(), stderr: se.String()}
	if ctx.Err() == context.DeadlineExceeded {
		o.timedOut = true
		o.exit = -1
		return o, nil
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			o.exit = ee.ExitCode()
			return o, nil
		}
		return o, err
	}
	return o, nil
}

// normStderr keeps stderr up to and including the first panic or fatal error line.
func normStderr(s string) string {
	lines := strings.SplitAfter(s, "\n")
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		if strings.HasPrefix(l, "panic: ") || strings.HasPrefix(l, "fatal error: ") {
			if !strings.HasSuffix(l, "\n") {
				b.WriteString("\n")
			}
			break
		}
	}
	return b.String()
}

type config struct {
	label   string
	o       string
	env     []string
	release bool
}

func buildConfigs() []config {
	if *flModes != "" {
		var cs []config
		for _, m := range strings.Split(*flModes, ",") {
			switch {
			case m == "dev":
				cs = append(cs, config{label: "dev"})
			case m == "release":
				cs = append(cs, config{label: "release", release: true})
			case m == "gcstress":
				// collect at every allocation opportunity: finds objects freed while still reachable
				cs = append(cs, config{label: "gcstress", env: []string{"MARMOT_GCSTRESS=1"}})
			case m == "poison":
				// freed memory is filled with a poison pattern, so a use after free shows at once
				cs = append(cs, config{label: "poison", env: []string{"MARMOT_POISON=1"}})
			case m == "evalorder":
				// the gc evaluation order scheduler switched on
				cs = append(cs, config{label: "evalorder", env: []string{"MARMOT_EVALORDER=1"}})
			case m == "lifetime":
				// every lifetime transform (in-place reuse, regions, frame regions) switched on
				cs = append(cs, config{label: "lifetime", env: []string{"MARMOT_LIFETIME=all"}})
			case m == "stress":
				cs = append(cs, config{label: "stress", env: []string{"MARMOT_GCSTRESS=1", "MARMOT_POISON=1"}})
			case strings.HasPrefix(m, "O"):
				cs = append(cs, config{label: m, o: strings.ToLower(m[1:])})
			}
		}
		return cs
	}
	if !*flMatrix {
		return []config{{o: *flO}}
	}
	cs := []config{{label: "O0", o: "0"}, {label: "O2", o: "2"}, {label: "Oz", o: "z"}}
	for _, kv := range strings.Split(*flBFlags, ";") {
		if label, flags, ok := strings.Cut(kv, "="); ok && label != "" {
			cs = append(cs, config{label: label, o: "2", env: []string{"MARMOT_BACKEND_FLAGS=" + flags}})
		}
	}
	return cs
}

func runOne(root, testsDir, work, marmot, name string, cfg config) result {
	start := time.Now()
	display := name
	if cfg.label != "" {
		display = name + "@" + cfg.label
	}
	r := result{Name: display, Dir: filepath.ToSlash(filepath.Dir(name)), Status: "fail"}
	defer func() { r.Seconds = time.Since(start).Seconds() }()

	gbase, srcDir, src := goldenBase(testsDir, name)
	wantOut, err := os.ReadFile(gbase + ".out")
	if err != nil {
		r.Reason, r.Detail = "no golden", "missing "+gbase+".out (run go run ./tests/conform -update)"
		return r
	}
	wantErr, _ := os.ReadFile(gbase + ".err")
	wantExit := 0
	if b, err := os.ReadFile(gbase + ".exit"); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &wantExit)
	}

	tdir := filepath.Join(work, strings.ReplaceAll(display, "/", "_"))
	os.RemoveAll(tdir)
	check(os.MkdirAll(tdir, 0o755))
	if !*flKeep {
		defer os.RemoveAll(tdir)
	}
	exe := filepath.Join(tdir, "prog")
	args := []string{"build", "-o", exe}
	if cfg.release {
		args = append(args, "-release")
	}
	if cfg.o != "" {
		args = append(args, "-O", cfg.o)
	}
	args = append(args, src)
	bto := *flBuildTO
	if strings.HasPrefix(name, "world/") {
		bto *= 5 // real modules can take minutes to compile on a cold object cache
	}
	bo, err := execute(bto, buildDir(root, srcDir, name), append(append([]string{"MARMOT_ROOT=" + root, "MARMOT_CACHE_MAX_MB=512"}, tier1Off()...), cfg.env...), marmot, args...)
	if err != nil {
		r.Reason, r.Detail = "compile error", err.Error()
		return r
	}
	if bo.timedOut {
		r.Reason = "compile timeout"
		return r
	}
	if bo.exit != 0 {
		r.Reason = "compile error"
		r.Detail = firstLines(bo.stdout+bo.stderr, 12)
		return r
	}
	ro, err := execute(*flTimeout, tdir, cfg.env, exe)
	if err != nil {
		r.Reason, r.Detail = "crash", err.Error()
		return r
	}
	if ro.timedOut {
		r.Reason = "timeout"
		return r
	}
	gotErr := normStderr(ro.stderr)
	switch {
	case ro.exit < 0 || ro.exit > 255 || (ro.exit != wantExit && ro.exit >= 128):
		r.Reason = fmt.Sprintf("crash (exit %d, want %d)", ro.exit, wantExit)
		r.Detail = firstLines(ro.stderr, 8)
	case ro.stdout != string(wantOut):
		r.Reason = "wrong output"
		r.Detail = diff(string(wantOut), ro.stdout)
	case gotErr != string(wantErr):
		r.Reason = "wrong stderr"
		r.Detail = diff(string(wantErr), gotErr)
	case ro.exit != wantExit:
		r.Reason = fmt.Sprintf("wrong exit status %d, want %d", ro.exit, wantExit)
	default:
		r.Status = "pass"
	}
	return r
}

// update regenerates goldens with the stock toolchain.
func update(root, testsDir, work string, tests []string) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, min(max(*flJ, 1), 4))
	var mu sync.Mutex
	failed := 0
	for _, name := range tests {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := updateOne(testsDir, work, name); err != nil {
				mu.Lock()
				failed++
				fmt.Printf("update %s: %v\n", name, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	fmt.Printf("updated %d goldens, %d failed\n", len(tests)-failed, failed)
	if failed > 0 {
		os.RemoveAll(work)
		os.Exit(1)
	}
}

func updateOne(testsDir, work, name string) error {
	gbase, srcDir, src := goldenBase(testsDir, name)
	tdir := filepath.Join(work, "ref_"+strings.ReplaceAll(name, "/", "_"))
	os.RemoveAll(tdir)
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tdir)
	exe := filepath.Join(tdir, "ref")
	bo, err := execute(*flBuildTO, srcDir, refEnv(name, srcDir), "go", "build", "-o", exe, src)
	if err != nil {
		return err
	}
	if bo.exit != 0 {
		return fmt.Errorf("go build failed: %s", firstLines(bo.stdout+bo.stderr, 6))
	}
	ro, err := execute(*flTimeout, tdir, nil, exe)
	if err != nil {
		return err
	}
	if ro.timedOut {
		return fmt.Errorf("stock go run timed out")
	}
	base := gbase
	write := func(ext, content string) error {
		if content == "" {
			err := os.Remove(base + ext)
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		return os.WriteFile(base+ext, []byte(content), 0o644)
	}
	if err := os.WriteFile(base+".out", []byte(ro.stdout), 0o644); err != nil {
		return err
	}
	if err := write(".err", normStderr(ro.stderr)); err != nil {
		return err
	}
	exit := ""
	if ro.exit != 0 {
		exit = fmt.Sprintf("%d\n", ro.exit)
	}
	return write(".exit", exit)
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	return strings.Join(lines, "\n")
}

// diff reports the first differing line with a little context.
func diff(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl || (i >= len(w)) != (i >= len(g)) {
			return fmt.Sprintf("line %d:\n  want: %q\n  got:  %q", i+1, wl, gl)
		}
	}
	return "outputs differ"
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}

type tally struct{ pass, fail, known int }

func tallies(results []result) (map[string]*tally, []string) {
	m := map[string]*tally{}
	var dirs []string
	for _, r := range results {
		t := m[r.Dir]
		if t == nil {
			t = &tally{}
			m[r.Dir] = t
			dirs = append(dirs, r.Dir)
		}
		switch {
		case r.Status == "pass":
			t.pass++
		case r.Known:
			t.known++
		default:
			t.fail++
		}
	}
	sort.Strings(dirs)
	return m, dirs
}

func printSummary(results []result) {
	m, dirs := tallies(results)
	var tp, tf, tk int
	for _, d := range dirs {
		t := m[d]
		n := t.pass + t.fail + t.known
		fmt.Printf("%-24s %4d/%-4d pass  %d fail  %d known\n", d, t.pass, n, t.fail, t.known)
		tp += t.pass
		tf += t.fail
		tk += t.known
	}
	fmt.Printf("%-24s %4d/%-4d pass  %d fail  %d known\n", "total", tp, tp+tf+tk, tf, tk)
}

func resultsMD(results []result, sha string) string {
	m, dirs := tallies(results)
	var b strings.Builder
	b.WriteString("# Conformance results\n\nGenerated by `go run ./tests/conform`. Do not edit. Compiler tree: git `" + sha + "`.\n\n")
	b.WriteString("| Directory | Pass | Total | Fail | Known failure |\n| --- | --- | --- | --- | --- |\n")
	var tp, tf, tk int
	for _, d := range dirs {
		t := m[d]
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |\n", d, t.pass, t.pass+t.fail+t.known, t.fail, t.known)
		tp += t.pass
		tf += t.fail
		tk += t.known
	}
	fmt.Fprintf(&b, "| total | %d | %d | %d | %d |\n", tp, tp+tf+tk, tf, tk)
	reasons := map[string]int{}
	for _, r := range results {
		if r.Status == "fail" {
			key := r.Reason
			if i := strings.Index(key, " ("); i >= 0 {
				key = key[:i]
			}
			reasons[key]++
		}
	}
	if len(reasons) > 0 {
		b.WriteString("\n## Failures by reason\n\n| Reason | Count |\n| --- | --- |\n")
		var keys []string
		for k := range reasons {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "| %s | %d |\n", k, reasons[k])
		}
	}
	return b.String()
}

func writeJSON(path string, results []result, sha string) {
	out := struct {
		Git     string   `json:"git"`
		Results []result `json:"results"`
	}{sha, results}
	data, err := json.MarshalIndent(out, "", "  ")
	check(err)
	check(os.WriteFile(path, append(data, '\n'), 0o644))
}

// killGroupOnCancel runs cmd in its own process group and kills the whole group when the
// context ends, so children of a hung compile or test never outlive it.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

func gitHead(root string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// snapshot copies what the compiler needs (build/bin, build/lib, lib/) into
// .work/tests/tree-SHA-PID so a long run is not disturbed by edits to the working tree.
// It smoke tests the copy with a hello program and removes older snapshots.
func snapshot(root, sha string) (string, error) {
	base := filepath.Join(root, ".work", "tests")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	olds, _ := filepath.Glob(filepath.Join(base, "tree-*"))
	for _, o := range olds {
		// Remove only snapshots whose owning process (the pid in the name) is gone.
		name := filepath.Base(o)
		pid := 0
		if i := strings.LastIndexByte(name, '-'); i >= 0 {
			fmt.Sscan(name[i+1:], &pid)
		}
		if pid <= 0 || syscall.Kill(pid, 0) != nil {
			os.RemoveAll(o)
		}
	}
	snap := filepath.Join(base, fmt.Sprintf("tree-%s-%d", sha, os.Getpid()))
	for _, d := range []string{"build/bin", "build/lib", "lib"} {
		dst := filepath.Join(snap, d)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if out, err := exec.Command("cp", "-R", filepath.Join(root, d), dst).CombinedOutput(); err != nil {
			return "", fmt.Errorf("snapshot copy %s: %v: %s", d, err, out)
		}
	}
	hello := filepath.Join(snap, "hello.go")
	if err := os.WriteFile(hello, []byte("package main\n\nfunc main() { println(\"hello\") }\n"), 0o644); err != nil {
		return "", err
	}
	exe := filepath.Join(snap, "hello")
	marmot := filepath.Join(snap, "build", "bin", "marmot")
	b, err := execute(*flBuildTO, snap, []string{"MARMOT_ROOT=" + snap, "MARMOT_CACHE_MAX_MB=512"}, marmot, "build", "-o", exe, hello)
	if err != nil || b.timedOut || b.exit != 0 {
		return "", fmt.Errorf("snapshot smoke test failed to build (the tree may be mid-edit, retry or use -live): %s", firstLines(b.stdout+b.stderr, 6))
	}
	r, err := execute(*flTimeout, snap, nil, exe)
	if err != nil || r.timedOut || r.exit != 0 || !strings.Contains(r.stderr, "hello") {
		return "", fmt.Errorf("snapshot smoke test failed to run (exit %d): %s", r.exit, firstLines(r.stderr, 4))
	}
	return snap, nil
}

// inDirs reports whether rel is under one of the comma separated directories in dirs.
func inDirs(rel, dirs string) bool {
	for _, d := range strings.Split(dirs, ",") {
		if d != "" && strings.HasPrefix(rel, strings.TrimSuffix(d, "/")+"/") {
			return true
		}
	}
	return false
}

// A world test with a file named "network" does not vendor its modules
// (mattn/go-sqlite3 would put megabytes of C in the repository): they come
// from the module cache, downloaded at the pinned versions in its go.sum.

func usesNetwork(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "network"))
	return err == nil
}

// modulesAvailable reports whether a network world test's modules are in
// the module cache or can be downloaded; other tests are always available.
func modulesAvailable(dir string) bool {
	if !usesNetwork(dir) {
		return true
	}
	cmd := exec.Command("go", "mod", "download")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	return cmd.Run() == nil
}

// refEnv is the environment of the stock Go build of a test.
func refEnv(name, srcDir string) []string {
	if strings.HasPrefix(name, "world/") && !usesNetwork(srcDir) {
		return []string{"GOFLAGS=-mod=vendor"}
	}
	return nil
}

// buildDir is the working directory for a compile: the module directory for world tests,
// the compiler tree otherwise.
func buildDir(root, srcDir, name string) string {
	if strings.HasPrefix(name, "world/") {
		return srcDir
	}
	return root
}

// tier1Off keeps background tier 1 out of the runs (it would compete for
// cores with the builds being measured) unless MARMOT_TIER1 is set.
func tier1Off() []string {
	if os.Getenv("MARMOT_TIER1") != "" {
		return nil
	}
	return []string{"MARMOT_TIER1=0"}
}
