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
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
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
	flJ       = flag.Int("j", runtime.NumCPU(), "parallel jobs")
	flUpdate  = flag.Bool("update", false, "regenerate goldens with the stock Go toolchain")
	flV       = flag.Bool("v", false, "print every test, with diff detail for failures")
	flJSON    = flag.String("json", "", "write JSON results to this file (default tests/results.json)")
	flO       = flag.String("O", "", "optimization level passed to marmot build -O")
	flKeep    = flag.Bool("keep", false, "keep the per-test work directory")
	flTimeout = flag.Duration("timeout", 10*time.Second, "run timeout per test")
	flBuildTO = flag.Duration("build-timeout", 120*time.Second, "compile timeout per test")
	flNoWrite = flag.Bool("nowrite", false, "do not write results.json and RESULTS.md")
)

func main() {
	flag.Parse()
	root, err := repoRoot()
	check(err)
	testsDir := filepath.Join(root, "tests")
	work := filepath.Join(root, ".work", "conform")
	check(os.MkdirAll(work, 0o755))

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

	marmot := filepath.Join(root, "build", "bin", "marmot")
	if _, err := os.Stat(marmot); err != nil {
		fmt.Fprintf(os.Stderr, "conform: %s not found (run make frontend); every test will fail\n", marmot)
	}
	known := loadKnown(filepath.Join(testsDir, "known_failures.txt"))

	results := make([]result, len(tests))
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(*flJ, 1))
	for i, t := range tests {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = runOne(root, testsDir, work, marmot, t)
			results[i].Known = known[t]
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
		writeJSON(jsonPath, results)
		check(os.WriteFile(filepath.Join(testsDir, "RESULTS.md"), []byte(resultsMD(results)), 0o644))
	} else if *flJSON != "" {
		writeJSON(*flJSON, results)
	}
	if unexpected > 0 {
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
		if _, err := os.Stat(filepath.Join(d, "SPEC.md")); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("cannot find repo root (SPEC.md) above %s", dir)
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
			if dir != "" && !strings.HasPrefix(rel, strings.TrimSuffix(dir, "/")+"/") {
				return nil
			}
			if filter != nil && !filter.MatchString(rel) {
				return nil
			}
			out = append(out, rel)
			return nil
		})
	}
	sort.Strings(out)
	return out
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

func runOne(root, testsDir, work, marmot, name string) result {
	start := time.Now()
	r := result{Name: name, Dir: filepath.ToSlash(filepath.Dir(name)), Status: "fail"}
	defer func() { r.Seconds = time.Since(start).Seconds() }()

	src := filepath.Join(testsDir, name+".go")
	wantOut, err := os.ReadFile(filepath.Join(testsDir, name+".out"))
	if err != nil {
		r.Reason, r.Detail = "no golden", "missing "+name+".out (run go run ./tests/conform -update)"
		return r
	}
	wantErr, _ := os.ReadFile(filepath.Join(testsDir, name+".err"))
	wantExit := 0
	if b, err := os.ReadFile(filepath.Join(testsDir, name+".exit")); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &wantExit)
	}

	tdir := filepath.Join(work, strings.ReplaceAll(name, "/", "_"))
	os.RemoveAll(tdir)
	check(os.MkdirAll(tdir, 0o755))
	if !*flKeep {
		defer os.RemoveAll(tdir)
	}
	exe := filepath.Join(tdir, "prog")
	args := []string{"build", "-o", exe}
	if *flO != "" {
		args = append(args, "-O", *flO)
	}
	args = append(args, src)
	bo, err := execute(*flBuildTO, root, []string{"MARMOT_ROOT=" + root}, marmot, args...)
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
	ro, err := execute(*flTimeout, tdir, nil, exe)
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
	sem := make(chan struct{}, max(*flJ, 1))
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
		os.Exit(1)
	}
}

func updateOne(testsDir, work, name string) error {
	src := filepath.Join(testsDir, name+".go")
	tdir := filepath.Join(work, "ref_"+strings.ReplaceAll(name, "/", "_"))
	os.RemoveAll(tdir)
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tdir)
	exe := filepath.Join(tdir, "ref")
	bo, err := execute(*flBuildTO, testsDir, nil, "go", "build", "-o", exe, src)
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
	base := filepath.Join(testsDir, name)
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

func resultsMD(results []result) string {
	m, dirs := tallies(results)
	var b strings.Builder
	b.WriteString("# Conformance results\n\nGenerated by `go run ./tests/conform`. Do not edit.\n\n")
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

func writeJSON(path string, results []result) {
	data, err := json.MarshalIndent(results, "", "  ")
	check(err)
	check(os.WriteFile(path, append(data, '\n'), 0o644))
}
