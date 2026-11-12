// Command agentfix measures how often a small model fixes the broken
// programs of tests/diag when it sees go build output, and when it sees
// marmot's JSON diagnostics instead.
//
// For each case and each condition the model gets the build output and
// main.go, and must answer with a fixed main.go. The fix is judged by go
// build (the same judge for both conditions) and by running it: its output
// and exit status must match those of the reference fix in fixed.go.txt, so
// a fix that compiles but changes what the program does (or keeps the bug,
// as with a shadowed err) does not count. A failed fix is retried with the
// new build output, up to -attempts times.
//
//	go run ./tests/diag/agentfix -model claude-haiku-5-5
//
// It calls the claude command line tool in print mode with no tools. The
// results go to tests/diag/agentfix/results.json and a summary to stdout.
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
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	flModel    = flag.String("model", "claude-haiku-5-5", "model for the claude CLI")
	flAttempts = flag.Int("attempts", 3, "attempts per case and condition")
	flJ        = flag.Int("j", 3, "parallel cases")
	flRun      = flag.String("run", "", "regexp: only cases whose name matches")
	flMarmot   = flag.String("marmot", "", "marmot binary (default build/bin/marmot)")
	flOut      = flag.String("o", "tests/diag/agentfix/results.json", "results file")
)

type attempt struct {
	Output   string `json:"output"` // build output the model saw
	Compiles bool   `json:"compiles"`
	Fixed    bool   `json:"fixed"` // compiles and behaves like the reference fix
	Seconds  int    `json:"seconds"`
}

type result struct {
	Case      string    `json:"case"`
	Condition string    `json:"condition"` // go or marmot
	Fixed     bool      `json:"fixed"`
	Compiles  bool      `json:"compiles"` // the last fix compiled
	Attempts  int       `json:"attempts"` // attempts used (to the fix, or all)
	Log       []attempt `json:"log"`
}

func main() {
	flag.Parse()
	os.Setenv("MARMOT_OUTPUT", "plain")
	root, _ := os.Getwd()
	marmot := *flMarmot
	if marmot == "" {
		marmot = filepath.Join(root, "build", "bin", "marmot")
	}
	cases, _ := filepath.Glob(filepath.Join(root, "tests/diag/testdata/*/main.go"))
	var re *regexp.Regexp
	if *flRun != "" {
		re = regexp.MustCompile(*flRun)
	}
	type job struct{ dir, cond string }
	var jobs []job
	for _, c := range cases {
		dir := filepath.Dir(c)
		if re != nil && !re.MatchString(filepath.Base(dir)) {
			continue
		}
		jobs = append(jobs, job{dir, "go"}, job{dir, "marmot"})
	}
	results := make([]result, len(jobs))
	sem := make(chan bool, *flJ)
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- true
			defer func() { <-sem }()
			results[i] = runCase(j.dir, j.cond, marmot, root)
			r := results[i]
			fmt.Fprintf(os.Stderr, "%-34s %-6s fixed=%v attempts=%d\n", r.Case, r.Condition, r.Fixed, r.Attempts)
		}()
	}
	wg.Wait()
	data, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile(*flOut, append(data, '\n'), 0o644)
	summary(results)
}

func runCase(dir, cond, marmot, root string) result {
	r := result{Case: filepath.Base(dir), Condition: cond}
	tmp, err := os.MkdirTemp("", "agentfix-")
	if err != nil {
		return r
	}
	defer os.RemoveAll(tmp)
	for _, f := range []string{"main.go", "go.mod"} {
		data, _ := os.ReadFile(filepath.Join(dir, f))
		os.WriteFile(filepath.Join(tmp, f), data, 0o644)
	}
	ref, _ := os.ReadFile(filepath.Join(dir, "fixed.go.txt"))
	want := behavior(string(ref))
	for a := 1; a <= *flAttempts; a++ {
		var out string
		if cond == "go" {
			out, _ = build(tmp, nil, "go", "build", "-o", os.DevNull, ".")
		} else {
			out, _ = build(tmp, []string{"MARMOT_OUTPUT=json", "MARMOT_ROOT=" + root, "MARMOT_DAEMON=0"}, marmot, "build", "-o", os.DevNull, ".")
		}
		src, _ := os.ReadFile(filepath.Join(tmp, "main.go"))
		start := time.Now()
		fixed := ask(out, string(src))
		at := attempt{Output: out, Seconds: int(time.Since(start).Seconds())}
		r.Attempts = a
		if fixed != "" {
			os.WriteFile(filepath.Join(tmp, "main.go"), []byte(fixed), 0o644)
			if _, ok := build(tmp, nil, "go", "build", "-o", os.DevNull, "."); ok && strings.Contains(fixed, "func main()") {
				at.Compiles = true
				at.Fixed = behavior(fixed) == want
			}
		}
		r.Log = append(r.Log, at)
		r.Compiles = at.Compiles
		if at.Fixed {
			r.Fixed = true
			break
		}
	}
	return r
}

// build runs a build in dir and returns its output with dir removed, and
// whether it succeeded.
func build(dir string, env []string, name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append([]string{"GOFLAGS=-mod=mod", "GOPROXY=off"}, env...)...)
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	out := strings.ReplaceAll(b.String(), dir+string(filepath.Separator), "")
	return strings.ReplaceAll(out, dir, "."), err == nil
}

// behavior runs a program and returns its output and exit status.
func behavior(src string) string {
	tmp, err := os.MkdirTemp("", "agentfix-run-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tmp)
	os.WriteFile(filepath.Join(tmp, "main.go"), []byte(src), 0o644)
	os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module x\n\ngo 1.27\n"), 0o644)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err = cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return fmt.Sprintf("%s\nexit %d", strings.ReplaceAll(b.String(), tmp, "."), code)
}

var codeBlock = regexp.MustCompile("(?s)```(?:go)?\\n(.*?)```")

const system = "You fix Go programs that do not compile. Answer with the complete fixed main.go in one ```go code block and nothing else. Keep what the program is meant to do; change as little as you can."

// ask gives the model the build output and the file and returns the fixed
// file, or "".
func ask(output, src string) string {
	prompt := "The Go program main.go does not compile. Build output:\n\n```\n" + output + "```\n\nmain.go:\n\n```go\n" + src + "```\n"
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "-p", "--model", *flModel, "--tools", "", "--no-session-persistence",
		"--setting-sources", "", "--system-prompt", system)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Dir = os.TempDir()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	m := codeBlock.FindStringSubmatch(string(out))
	if m == nil {
		return ""
	}
	return m[1]
}

func summary(rs []result) {
	type agg struct{ cases, fixed, first, attempts, compiles int }
	by := map[string]*agg{"go": {}, "marmot": {}}
	for _, r := range rs {
		a := by[r.Condition]
		a.cases++
		a.attempts += r.Attempts
		if r.Compiles {
			a.compiles++
		}
		if r.Fixed {
			a.fixed++
			if r.Attempts == 1 {
				a.first++
			}
		}
	}
	fmt.Println("| output the model saw | cases | fixed correctly | on the first try | attempts per case | compiles but wrong |")
	fmt.Println("| --- | --- | --- | --- | --- | --- |")
	conds := []string{"go", "marmot"}
	sort.Strings(conds)
	for _, c := range conds {
		a := by[c]
		if a.cases == 0 {
			continue
		}
		name := map[string]string{"go": "go build", "marmot": "marmot JSON"}[c]
		fmt.Printf("| %s | %d | %d (%.0f%%) | %d (%.0f%%) | %.2f | %d |\n", name, a.cases, a.fixed, pct(a.fixed, a.cases), a.first, pct(a.first, a.cases), float64(a.attempts)/float64(a.cases), a.compiles-a.fixed)
	}
}

func pct(a, b int) float64 { return 100 * float64(a) / float64(b) }
