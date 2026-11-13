package diag

import (
	"bufio"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// TestResult is one test (or subtest) of a test binary run with -test.v.
type TestResult struct {
	Kind       string   `json:"kind"` // "test"
	Schema     int      `json:"schema"`
	Package    string   `json:"package"`
	Name       string   `json:"name"`
	Status     string   `json:"status"` // pass, fail, skip
	DurationMs int64    `json:"durationMs"`
	File       string   `json:"file,omitempty"` // first failure location
	Line       int      `json:"line,omitempty"`
	Message    string   `json:"message,omitempty"` // the first failure message, with got/want lines
	Output     []string `json:"output,omitempty"`  // everything the test logged
}

var (
	reRun    = lazy(`^=== (RUN|CONT|PAUSE|NAME)\s+(\S+)`)
	reDone   = lazy(`^\s*--- (PASS|FAIL|SKIP): (\S+) \((\d+(?:\.\d+)?)s\)`)
	reLogLoc = lazy(`^\s+([\w./\-]+\.go):(\d+): (.*)$`)
)

// ParseTestOutput reads the -test.v output of a test binary. Every line is
// also passed to echo when it is not nil. Results are in completion order.
// A panic ends the run: the running test is marked failed with the panic
// message.
func ParseTestOutput(r io.Reader, pkg, dir string, echo func(string)) []TestResult {
	var out []TestResult
	byName := map[string]*TestResult{}
	var order []string
	cur := ""
	get := func(name string) *TestResult {
		t := byName[name]
		if t == nil {
			t = &TestResult{Kind: "test", Schema: Schema, Package: pkg, Name: name}
			byName[name] = t
			order = append(order, name)
		}
		return t
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	panicking := false
	var panicLines []string
	for sc.Scan() {
		l := sc.Text()
		if echo != nil {
			echo(l)
		}
		if panicking {
			panicLines = append(panicLines, l)
			continue
		}
		if m := reRun.FindStringSubmatch(l); m != nil {
			cur = m[2]
			get(cur)
			continue
		}
		if m := reDone.FindStringSubmatch(l); m != nil {
			t := get(m[2])
			t.Status = strings.ToLower(m[1])
			sec, _ := strconv.ParseFloat(m[3], 64)
			t.DurationMs = int64(sec * 1000)
			cur = m[2]
			continue
		}
		if strings.HasPrefix(l, "panic: ") && cur != "" {
			panicking = true
			panicLines = append(panicLines, l)
			continue
		}
		if cur == "" || l == "PASS" || l == "FAIL" || strings.HasPrefix(l, "ok  \t") || strings.HasPrefix(l, "FAIL\t") {
			continue
		}
		t := get(cur)
		text := strings.TrimPrefix(l, "    ")
		t.Output = append(t.Output, text)
		if m := reLogLoc.FindStringSubmatch(l); m != nil && t.File == "" {
			t.File = m[1]
			if !filepath.IsAbs(t.File) && dir != "" {
				t.File = filepath.Join(dir, t.File)
			}
			t.Line, _ = strconv.Atoi(m[2])
			t.Message = m[3]
		} else if t.File != "" && len(t.Output) > 1 && strings.HasPrefix(l, "        ") && t.Message != "" {
			// Continuation lines of a multi-line message (got/want).
			if strings.Count(t.Message, "\n") < 20 {
				t.Message += "\n" + strings.TrimSpace(l)
			}
		}
	}
	if panicking && cur != "" {
		t := get(cur)
		t.Status = "fail"
		t.Message = strings.TrimPrefix(panicLines[0], "panic: ")
		t.Output = append(t.Output, panicLines...)
		// The first frame in a _test.go file locates the panic.
		for i, l := range panicLines {
			if m := posRE.FindStringSubmatch(strings.TrimSpace(strings.SplitN(l, " +0x", 2)[0]) + ": x"); m != nil && i > 0 && strings.HasSuffix(m[1], "_test.go") {
				t.File = m[1]
				t.Line, _ = strconv.Atoi(m[2])
				break
			}
		}
	}
	for _, n := range order {
		t := byName[n]
		if t.Status == "" {
			t.Status = "fail" // never finished
		}
		out = append(out, *t)
	}
	return out
}

// TestDiagnostics turns failed tests into diagnostics (code M0022) with the
// failure location as the span. Parent tests that failed only because a
// subtest failed are left out.
func TestDiagnostics(ts []TestResult) []Diagnostic {
	var ds []Diagnostic
	for _, t := range ts {
		if t.Status != "fail" {
			continue
		}
		if t.File == "" && hasFailedChild(ts, t.Name) {
			continue
		}
		msg := t.Name + " failed"
		if t.Message != "" {
			first, rest, _ := strings.Cut(t.Message, "\n")
			msg = t.Name + ": " + first
			d := Diagnostic{Kind: "diagnostic", Severity: Error, Code: CodeTestFail, Message: msg, Package: t.Package}
			if t.File != "" {
				d.Span = Span{File: t.File, Line: t.Line, Col: 1}
			}
			for r := range strings.SplitSeq(rest, "\n") {
				if r != "" {
					d.Notes = append(d.Notes, r)
				}
			}
			ds = append(ds, d)
			continue
		}
		ds = append(ds, Diagnostic{Kind: "diagnostic", Severity: Error, Code: CodeTestFail, Message: msg, Package: t.Package})
	}
	return ds
}

func hasFailedChild(ts []TestResult, name string) bool {
	for _, t := range ts {
		if t.Status == "fail" && strings.HasPrefix(t.Name, name+"/") {
			return true
		}
	}
	return false
}
