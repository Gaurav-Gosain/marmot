package diag

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// TestGolden renders each testdata case (main.go plus the compiler output in
// err.txt) in plain, color and JSON form and compares with the goldens.
func TestGolden(t *testing.T) {
	cases, _ := filepath.Glob("testdata/*/err.txt")
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	goroot := testGOROOT(t)
	for _, c := range cases {
		dir, _ := filepath.Abs(filepath.Dir(c))
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			text, err := os.ReadFile(c)
			if err != nil {
				t.Fatal(err)
			}
			render := func(style Style, mode Mode) string {
				src := NewSources()
				src.GOROOT = goroot
				ds := Process(string(text), src, Options{Context: mode == ModeJSON, Rendered: mode == ModeJSON, Dir: dir})
				var b bytes.Buffer
				if mode == ModeJSON {
					WriteJSON(&b, ds)
					WriteResult(&b, Result{Command: "build", Errors: len(ds)})
				} else {
					(&Human{W: &b, Style: style, Dir: dir, Sources: src}).Render(ds)
				}
				return strings.ReplaceAll(b.String(), dir, "$DIR")
			}
			check(t, filepath.Join(dir, "human.golden"), render(Plain, ModeHuman))
			check(t, filepath.Join(dir, "color.golden"), render(Colors, ModeHuman))
			js := render(Plain, ModeJSON)
			check(t, filepath.Join(dir, "json.golden"), js)
			checkSchema(t, js)
		})
	}
}

func testGOROOT(t *testing.T) string {
	s := NewSources()
	r := s.goroot()
	if r == "" {
		t.Skip("no GOROOT")
	}
	return r
}

func check(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs (go test -update to accept)\n--- got\n%s\n--- want\n%s", filepath.Base(path), got, want)
	}
}

// checkSchema validates the JSON Lines against schema 1: every line is an
// object with kind and schema, diagnostics have the required fields, and the
// last line is the result.
func checkSchema(t *testing.T, js string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(js), "\n")
	for i, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		if m["schema"] != float64(Schema) {
			t.Errorf("line %d: schema %v", i+1, m["schema"])
		}
		kind, _ := m["kind"].(string)
		last := i == len(lines)-1
		switch {
		case last && kind != "result":
			t.Errorf("last line kind %q, want result", kind)
		case !last && kind != "diagnostic":
			t.Errorf("line %d kind %q, want diagnostic", i+1, kind)
		}
		if kind == "diagnostic" {
			for _, k := range []string{"severity", "code", "message", "span"} {
				if _, ok := m[k]; !ok {
					t.Errorf("line %d: no %s", i+1, k)
				}
			}
			if c, _ := m["code"].(string); Lookup(c) == nil {
				t.Errorf("line %d: unknown code %q", i+1, c)
			}
			if fx, ok := m["fixes"].([]any); ok {
				for _, f := range fx {
					edits, _ := f.(map[string]any)["edits"].([]any)
					if len(edits) == 0 {
						t.Errorf("line %d: fix without edits", i+1)
					}
				}
			}
		}
		if kind == "result" {
			for _, k := range []string{"ok", "errors", "warnings", "durationMs"} {
				if _, ok := m[k]; !ok {
					t.Errorf("result: no %s", k)
				}
			}
		}
	}
}

// TestWrapWidths renders cases with long messages, notes, help and labels
// at terminal widths 60, 80 and 120 (wrapping caps at 100).
func TestWrapWidths(t *testing.T) {
	goroot := testGOROOT(t)
	var ds []Diagnostic
	dirs := map[string]string{}
	for _, name := range []string{"iface", "method", "mismatch"} {
		dir, _ := filepath.Abs(filepath.Join("testdata", name))
		text, err := os.ReadFile(filepath.Join(dir, "err.txt"))
		if err != nil {
			t.Fatal(err)
		}
		src := NewSources()
		src.GOROOT = goroot
		got := Process(string(text), src, Options{Dir: dir})
		for i := range got {
			dirs[got[i].Span.File] = dir
		}
		ds = append(ds, got...)
	}
	// A long label under the carets, and one that hangs below another.
	mm, _ := filepath.Abs("testdata/mismatch/main.go")
	ds = append(ds, Diagnostic{Kind: "diagnostic", Severity: Error, Code: "M0006",
		Message: "a synthetic error with a long label to show how labels wrap",
		Span:    Span{File: mm, Line: 9, Col: 20, EndLine: 9, EndCol: 21},
		Primary: "this label is long enough that it has to wrap at the narrower widths, and `a code span stays whole`",
		Labels: []Label{{Span: Span{File: mm, Line: 9, Col: 6, EndLine: 9, EndCol: 10},
			Text: "and a second label, also long, that hangs below the first one and wraps as well"}}})
	for _, w := range []int{60, 80, 120} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			var b bytes.Buffer
			src := NewSources()
			src.GOROOT = goroot
			h := &Human{W: &b, Style: Plain, Sources: src, Width: w, Max: 100}
			for i := range ds {
				h.Dir = dirs[ds[i].Span.File]
				h.RenderOne(&ds[i])
			}
			out := b.String()
			for _, l := range strings.Split(out, "\n") {
				if n := visible(l); n > min(w, maxWidth) && !strings.Contains(l, " | ") && !strings.Contains(l, "-->") {
					// Source lines and paths are not wrapped; everything else is.
					t.Errorf("line of %d columns at width %d: %q", n, w, l)
				}
			}
			dir, _ := filepath.Abs("testdata")
			check(t, filepath.Join("testdata", fmt.Sprintf("wrap%d.golden", w)), strings.ReplaceAll(out, dir, "$DIR"))
		})
	}
}
