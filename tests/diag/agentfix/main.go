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
// new build output while it does not compile, up to -attempts times.
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
	flOut      = flag.String("o", "", "results file (default tests/diag/agentfix/results.json, or results-hard.json with -hard)")
	flHard     = flag.Bool("hard", false, "use tests/diag/hard: modules of several files and packages; the model sees only the failing file")
)

type attempt struct {
	Output   string `json:"output"` // build output the model saw
	Compiles bool   `json:"compiles"`
	Fixed    bool   `json:"fixed"` // compiles and behaves like the reference fix
	Seconds  int    `json:"seconds"`
	Fix      string `json:"fix,omitempty"` // the file the model answered with
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
	corpus := "tests/diag/testdata"
	if *flHard {
		corpus = "tests/diag/hard"
	}
	if *flOut == "" {
		*flOut = "tests/diag/agentfix/results.json"
		if *flHard {
			*flOut = "tests/diag/agentfix/results-hard.json"
		}
	}
	cases, _ := filepath.Glob(filepath.Join(root, corpus, "*/go.mod"))
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
	// Copy the module (every .go file and go.mod, keeping directories).
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if n := d.Name(); n == "go.mod" || strings.HasSuffix(n, ".go") {
			rel, _ := filepath.Rel(dir, p)
			os.MkdirAll(filepath.Dir(filepath.Join(tmp, rel)), 0o755)
			data, _ := os.ReadFile(p)
			os.WriteFile(filepath.Join(tmp, rel), data, 0o644)
		}
		return nil
	})
	failing := "main.go"
	if b, err := os.ReadFile(filepath.Join(dir, "failing")); err == nil {
		failing = strings.TrimSpace(string(b))
	}
	ref, _ := os.ReadFile(filepath.Join(dir, "fixed.go.txt"))
	want := behaviorIn(tmp, failing, string(ref))
	// Cases with several sound fixes (which argument to add, which
	// declaration to keep) are judged by compiling only.
	_, err = os.Stat(filepath.Join(dir, "ambiguous"))
	ambiguous := err == nil
	for a := 1; a <= *flAttempts; a++ {
		var out string
		if cond == "go" {
			out, _ = build(tmp, nil, "go", "build", "-o", os.DevNull, ".")
		} else {
			out, _ = build(tmp, []string{"MARMOT_OUTPUT=json", "MARMOT_ROOT=" + root, "MARMOT_DAEMON=0"}, marmot, "build", "-o", filepath.Join(tmp, "prog"), ".")
		}
		src, _ := os.ReadFile(filepath.Join(tmp, failing))
		start := time.Now()
		fixed := ask(out, failing, string(src), *flHard)
		at := attempt{Output: out, Seconds: int(time.Since(start).Seconds()), Fix: fixed}
		r.Attempts = a
		if fixed != "" {
			os.WriteFile(filepath.Join(tmp, failing), []byte(fixed), 0o644)
			if _, ok := build(tmp, nil, "go", "build", "-o", os.DevNull, "."); ok && strings.Contains(fixed, "func main()") {
				at.Compiles = true
				at.Fixed = ambiguous || behaviorIn(tmp, failing, fixed) == want
			}
		}
		r.Log = append(r.Log, at)
		r.Compiles = at.Compiles
		r.Fixed = at.Fixed
		if at.Compiles {
			// The model only sees build output: once the program builds
			// there is nothing more to tell it.
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
	return scrub(b.String(), dir), err == nil
}

var tempRE = regexp.MustCompile(`(/private)?` + regexp.QuoteMeta(strings.TrimSuffix(os.TempDir(), "/")) + `/[\w.-]+`)

// scrub removes the case directory and temporary paths from build output
// (macOS reports /private/var/... for /var/...).
func scrub(out, dir string) string {
	for _, d := range []string{"/private" + dir, dir} {
		out = strings.ReplaceAll(out, d+string(filepath.Separator), "")
		out = strings.ReplaceAll(out, d, ".")
	}
	return tempRE.ReplaceAllString(out, "$$TMPDIR")
}

// behaviorIn runs the module in dir with file replaced by src and returns
// the output and exit status; the file is restored after.
func behaviorIn(dir, file, src string) string {
	path := filepath.Join(dir, file)
	old, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(src), 0o644)
	defer os.WriteFile(path, old, 0o644)
	tmp := dir
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return fmt.Sprintf("%s\nexit %d", strings.ReplaceAll(b.String(), tmp, "."), code)
}

var codeBlock = regexp.MustCompile("(?s)```(?:go)?\\n(.*?)```")

const system = "You fix Go programs that do not compile. Answer with the complete fixed file in one ```go code block and nothing else. Keep what the program is meant to do; change as little as you can."

// ask gives the model the build output and the file and returns the fixed
// file, or "".
func ask(output, file, src string, partial bool) string {
	prompt := "The Go program does not compile. Build output:\n\n```\n" + output + "```\n\n" + file + ":\n\n```go\n" + src + "```\n"
	if partial {
		prompt += "\nOnly " + file + " is shown and only " + file + " may change; the other files of the module stay as they are. Answer with the complete fixed " + file + ".\n"
	}
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
