package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

// runTestHuman runs a test binary for a terminal: failures first, each with
// its source line and the test's log, then the tests as a tree with status
// and time, then a summary line.
func runTestHuman(rep *diag.Reporter, cmd *exec.Cmd, pkg string, start time.Time) int {
	verbose := false
	for _, a := range cmd.Args[1:] {
		if a == "-test.v" || strings.HasPrefix(a, "-test.v=") {
			verbose = true
		}
	}
	if !verbose {
		cmd.Args = append(cmd.Args, "-test.v")
	}
	cmd.Env = append(os.Environ(), "MARMOT_OUTPUT=plain")
	pr, pw := io.Pipe()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, pw, pw
	if err := cmd.Start(); err != nil {
		return rep.Fail(err)
	}
	done := make(chan []diag.TestResult)
	go func() { done <- diag.ParseTestOutput(pr, pkg, cmd.Dir, nil) }()
	err := cmd.Wait()
	_ = pw.Close() // io.PipeWriter.Close always returns nil
	results := <-done
	elapsed := time.Since(start)
	st := diag.Plain
	if rep.Color {
		st = diag.Colors
	}
	w := os.Stdout
	ds := diag.Finish(diag.TestDiagnostics(results), diag.NewSources(), diag.Options{Dir: rep.Dir})
	h := &diag.Human{W: w, Style: st, Dir: rep.Dir, Max: 1 << 30}
	byName := map[string]diag.TestResult{}
	for _, t := range results {
		byName[t.Name] = t
	}
	// Failures first, with source and log.
	for _, d := range ds {
		h.RenderOne(&d)
		name, _, _ := strings.Cut(d.Message, ": ")
		if t, ok := byName[name]; ok {
			// The log, without the lines the diagnostic already shows.
			var extra []string
			for _, l := range t.Output {
				text := strings.TrimSpace(l)
				if i := strings.Index(text, ".go:"); i >= 0 {
					if j := strings.Index(text[i:], ": "); j >= 0 {
						text = text[i+j+2:]
					}
				}
				if !strings.Contains(t.Message, text) {
					extra = append(extra, l)
				}
			}
			if len(extra) > 0 {
				fmt.Fprintf(w, "%s  log:%s\n", st.Dim, st.Reset)
				for _, l := range extra {
					fmt.Fprintf(w, "%s    %s%s\n", st.Dim, l, st.Reset)
				}
			}
		}
		fmt.Fprintln(w)
	}
	// The tree.
	names := make([]string, 0, len(results))
	for _, t := range results {
		names = append(names, t.Name)
	}
	sort.SliceStable(names, func(i, j int) bool { return order(results, names[i]) < order(results, names[j]) })
	passed, failed, skipped := 0, 0, 0
	for _, n := range names {
		t := byName[n]
		depth := strings.Count(n, "/")
		label := n[strings.LastIndex(n, "/")+1:]
		status, color := "ok  ", st.Help
		switch t.Status {
		case "fail":
			status, color = "FAIL", st.Error
			if depth == 0 {
				failed++
			}
		case "skip":
			status, color = "skip", st.Warning
			if depth == 0 {
				skipped++
			}
		default:
			if depth == 0 {
				passed++
			}
		}
		fmt.Fprintf(w, "%s%s%s%s %s %s(%s)%s\n", strings.Repeat("  ", depth), color, status, st.Reset, label, st.Dim, ms(t.DurationMs), st.Reset)
	}
	code := 0
	if err != nil {
		code = 1
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() > 0 {
			code = ee.ExitCode()
		}
	}
	summary := fmt.Sprintf("%d passed", passed)
	if failed > 0 {
		summary += fmt.Sprintf(", %d failed", failed)
	}
	if skipped > 0 {
		summary += fmt.Sprintf(", %d skipped", skipped)
	}
	color, word := st.Help, "ok  "
	if code != 0 {
		color, word = st.Error, "FAIL"
	}
	fmt.Fprintf(w, "\n%s%s%s %s: %s in %s\n", color, word, st.Reset, pkg, summary, elapsed.Round(time.Millisecond))
	return code
}

// order keeps tests in the order they started, subtests under their parent.
func order(rs []diag.TestResult, name string) string {
	top, _, _ := strings.Cut(name, "/")
	for i, t := range rs {
		if t.Name == top {
			return fmt.Sprintf("%06d/%s", i, name)
		}
	}
	return "999999/" + name
}

func ms(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%dms", n)
	}
	return fmt.Sprintf("%.2fs", float64(n)/1000)
}
