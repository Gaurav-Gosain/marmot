package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	env := func(kv ...string) Env {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name  string
		json  bool
		color string
		env   Env
		tty   bool
		mode  Mode
		col   bool
	}{
		{"pipe", false, "auto", env(), false, ModePlain, false},
		{"tty", false, "auto", env(), true, ModeHuman, true},
		{"tty no color", false, "auto", env("NO_COLOR", "1"), true, ModeHuman, false},
		{"tty never", false, "never", env(), true, ModeHuman, false},
		{"forced human", false, "always", env("MARMOT_OUTPUT", "human"), false, ModeHuman, true},
		{"flag", true, "auto", env(), true, ModeJSON, false},
		{"env json", false, "auto", env("MARMOT_OUTPUT", "json"), true, ModeJSON, false},
		{"agent", false, "auto", env("CLAUDECODE", "1"), true, ModeJSON, false},
		{"agent off", false, "auto", env("CLAUDECODE", "0"), false, ModePlain, false},
		{"agent overridden", false, "auto", env("CLAUDECODE", "1", "MARMOT_OUTPUT", "plain"), true, ModePlain, false},
	}
	for _, c := range cases {
		s := Resolve(c.json, c.color, c.env, c.tty)
		if s.Mode != c.mode || s.Color != c.col {
			t.Errorf("%s: got mode %d color %v, want %d %v", c.name, s.Mode, s.Color, c.mode, c.col)
		}
	}
}

func TestClassify(t *testing.T) {
	for msg, want := range map[string]string{
		"undefined: x":                                        CodeUndefined,
		`"os" imported and not used`:                          CodeUnusedImport,
		`"math/rand" imported as r and not used`:              CodeUnusedImport,
		"declared and not used: v":                            CodeUnusedVar,
		"expected ';', found 'EOF'":                           CodeSyntax,
		"missing return":                                      CodeMissingReturn,
		"x.f undefined (type T has no field or method f)":     CodeNoFieldMethod,
		"marmot-backend failed: signal: abort trap\nLLVM ERR": CodeInternal,
		"something new":                                       CodeOther,
	} {
		if got := Classify(msg); got != want {
			t.Errorf("Classify(%q) = %s, want %s", msg, got, want)
		}
	}
}

func TestClosest(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"Printn", "Println"},
		{"Sprintf", ""}, // exact match is not a suggestion
		{"printf", "Printf"},
		{"Xyzzy", ""},
	} {
		got := closest(c.name, []string{"Print", "Println", "Printf", "Sprintf"})
		if got != c.want {
			t.Errorf("closest(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestApplyFixes applies the fixes of the multi case to a copy and checks
// the result.
func TestApplyFixes(t *testing.T) {
	dir := t.TempDir()
	src, _ := os.ReadFile("testdata/multi/main.go")
	text, _ := os.ReadFile("testdata/multi/err.txt")
	path := filepath.Join(dir, "main.go")
	os.WriteFile(path, src, 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n"), 0o644)
	ds := Dedup(FromText(string(text), dir))
	s := NewSources()
	s.Enrich(ds, Options{})
	applied, err := ApplyFixes(ds, map[string]bool{CodeUnusedImport: true, CodeUndefined: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 3 {
		t.Errorf("applied %d fixes, want 3: %+v", len(applied), applied)
	}
	got, _ := os.ReadFile(path)
	want := `package main

import (
	"fmt"
	"strings"
)

func add(a, b int) int {
	return a + b
}

func main() {
	count := 3
	fmt.Println(add(1, "two"))
	fmt.Println("x")
	x := strings.ToUpper("a")
	return 1
}
`
	if string(got) != want {
		t.Errorf("fixed file:\n%s\nwant:\n%s", got, want)
	}
}

func TestFromTextInternal(t *testing.T) {
	ds := FromText("marmot-backend failed: signal: abort trap\nLLVM ERROR: bad\nin function main.f\n", "/x")
	if len(ds) != 1 || ds[0].Code != CodeInternal || len(ds[0].Notes) != 2 {
		t.Fatalf("got %+v", ds)
	}
}

func TestParseTestOutput(t *testing.T) {
	out := `=== RUN   TestAdd
    a_test.go:7: Add(2, 2) = 0, want 4
--- FAIL: TestAdd (0.01s)
=== RUN   TestOK
--- PASS: TestOK (0.00s)
=== RUN   TestSub
=== RUN   TestSub/zero
    a_test.go:16: bad
        got: -1
        want: 1
--- FAIL: TestSub (0.00s)
    --- FAIL: TestSub/zero (0.00s)
=== RUN   TestBoom
panic: boom [recovered]
	panic: boom

goroutine 5 [running]:
tt.TestBoom(...)
	/src/tt/a_test.go:30 +0x1c
FAIL
`
	ts := ParseTestOutput(strings.NewReader(out), "tt", "/src/tt", nil)
	got := map[string]TestResult{}
	for _, r := range ts {
		got[r.Name] = r
	}
	if r := got["TestAdd"]; r.Status != "fail" || r.Line != 7 || r.File != "/src/tt/a_test.go" || r.DurationMs != 10 {
		t.Errorf("TestAdd: %+v", r)
	}
	if r := got["TestOK"]; r.Status != "pass" {
		t.Errorf("TestOK: %+v", r)
	}
	if r := got["TestSub/zero"]; r.Message != "bad\ngot: -1\nwant: 1" {
		t.Errorf("TestSub/zero message %q", r.Message)
	}
	if r := got["TestBoom"]; r.Status != "fail" || r.Message != "boom [recovered]" || r.Line != 30 {
		t.Errorf("TestBoom: %+v", r)
	}
	ds := TestDiagnostics(ts)
	if len(ds) != 3 {
		t.Errorf("%d diagnostics, want 3 (TestSub folds into its subtest)", len(ds))
	}
}
