package check

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

func TestParseNolint(t *testing.T) {
	for _, c := range []struct {
		text    string
		linters []string
		ok      bool
	}{
		{"//nolint", nil, true},
		{"// nolint", nil, true},
		{"//nolint:errcheck", []string{"errcheck"}, true},
		{"//nolint:errcheck,gosec // reason", []string{"errcheck", "gosec"}, true},
		{"//nolint:all", nil, true},
		{"//nolintx", nil, false},
		{"// not a directive", nil, false},
	} {
		got, ok := parseNolint(c.text)
		if ok != c.ok || !slices.Equal(got, c.linters) {
			t.Errorf("parseNolint(%q) = %v, %v; want %v, %v", c.text, got, ok, c.linters, c.ok)
		}
	}
}

func TestFilterChecks(t *testing.T) {
	names := []string{"SA1000", "SA4006", "S1000", "ST1000", "ST1003", "QF1001"}
	on := filterChecks(names, []string{"all", "-ST1000", "-QF*", "-SA4*"})
	var got []string
	for _, n := range names {
		if on[n] {
			got = append(got, n)
		}
	}
	if want := []string{"SA1000", "S1000", "ST1003"}; !slices.Equal(got, want) {
		t.Errorf("enabled %v, want %v", got, want)
	}
	// S* is the category S, not SA or ST.
	on = filterChecks(names, []string{"S*"})
	if !on["S1000"] || on["SA1000"] || on["ST1000"] {
		t.Errorf("S* matched %v", on)
	}
}

func TestGoAtLeast(t *testing.T) {
	for _, c := range []struct {
		v, min string
		want   bool
	}{{"1.22", "1.22", true}, {"1.21.5", "1.22", false}, {"1.26", "1.22", true}, {"", "1.22", true}} {
		if got := goAtLeast(c.v, c.min); got != c.want {
			t.Errorf("goAtLeast(%q, %q) = %v", c.v, c.min, got)
		}
	}
}

// findings lists a result as sorted "check analyzer file:line" strings.
func findings(r *Result) []string {
	var out []string
	for _, c := range r.Checks {
		for _, d := range c.Findings {
			out = append(out, fmt.Sprintf("%s %s %s:%d", c.Name, d.Analyzer, filepath.Base(d.Span.File), d.Span.Line))
		}
	}
	return out
}

func TestRunModule(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	dir, _ := filepath.Abs("testdata/mod")
	cache := t.TempDir()
	opts := Options{Dir: dir, Only: []string{"fmt", "vet", "lint", "modernize"}, CacheDir: cache, Jobs: 2}
	r1, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	got := findings(r1)
	want := []string{
		"fmt gofmt b.go:3",
		"vet printf a.go:11",
		"lint printf a.go:11",
		"lint errcheck a.go:17",
		"lint unused a.go:25",
		"modernize rangeint a.go:30",
	}
	if !slices.Equal(got, want) {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// errcheck and gosec report on the same line; golangci-lint keeps the
	// first linter by name.
	for _, c := range r1.Checks {
		if c.Name == "lint" && c.Status != Failed {
			t.Errorf("lint status %s, want failed", c.Status)
		}
		if c.Name == "modernize" && c.Status != Findings {
			t.Errorf("modernize status %s, want findings (report only)", c.Status)
		}
	}
	if r1.Stats.Hits != 0 || r1.Stats.Misses == 0 {
		t.Errorf("first run: %d hits, %d misses", r1.Stats.Hits, r1.Stats.Misses)
	}
	// The second run is answered from the cache with the same result.
	r2, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Stats.Misses != 0 || r2.Stats.Analyzed != 0 {
		t.Errorf("second run: %d misses, %d analyzed", r2.Stats.Misses, r2.Stats.Analyzed)
	}
	if g2 := findings(r2); !slices.Equal(g2, got) {
		t.Errorf("cached run differs:\n%s", strings.Join(g2, "\n"))
	}
	// The modernizer's fix is machine applicable.
	for _, c := range r1.Checks {
		for _, d := range c.Findings {
			if c.Name == "modernize" && (len(d.Fixes) == 0 || d.Fixes[0].Applicability != "machine-applicable") {
				t.Errorf("modernize finding without a machine applicable fix: %+v", d)
			}
		}
	}
}

func TestMayNeedImports(t *testing.T) {
	for _, c := range []struct {
		src  string
		want bool
	}{
		{"package p\nimport \"strings\"\nvar _ = strings.ToUpper", false},
		{"package p\nimport \"strings\"\nvar x = 1", true}, // unused import
		{"package p\nvar _ = strings.ToUpper", true},       // missing import
		{"package p\nvar _ = local.Field", false},          // declared in a sibling
		{"package p\nimport yaml \"go.yaml.in/yaml/v3\"\nvar _ = yaml.Marshal", false},
		{"package p\nimport \"go.yaml.in/yaml/v3\"\nvar _ = yaml.Marshal", false}, // major version suffix
	} {
		if got := mayNeedImports([]byte(c.src), map[string]bool{"local": true}); got != c.want {
			t.Errorf("mayNeedImports(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

func TestRunCross(t *testing.T) {
	if testing.Short() {
		t.Skip("type checks the standard library for two targets")
	}
	t.Setenv("GOWORK", "off")
	dir, _ := filepath.Abs("testdata/mod")
	r, err := Run(Options{Dir: dir, Only: []string{"cross"}, CacheDir: t.TempDir(), Targets: []string{"linux/amd64", "windows/amd64"}})
	if err != nil {
		t.Fatal(err)
	}
	c := r.Checks[0]
	if c.Status != Failed || len(c.Findings) != 1 {
		t.Fatalf("cross: %s with %d findings, want one failure: %+v", c.Status, len(c.Findings), c.Findings)
	}
	d := c.Findings[0]
	if filepath.Base(d.Span.File) != "w_windows.go" || d.Analyzer != "windows/amd64" {
		t.Errorf("finding %s:%d from %s, want w_windows.go from windows/amd64", d.Span.File, d.Span.Line, d.Analyzer)
	}
}

func TestRunDeadcode(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	t.Setenv("GOWORK", "off")
	dir, _ := filepath.Abs("testdata/dead")
	r, err := Run(Options{Dir: dir, Only: []string{"deadcode"}, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	c := r.Checks[0]
	if c.Status != Findings || len(c.Findings) != 1 || c.Findings[0].Message != "unreachable func: Dead" {
		t.Fatalf("deadcode: %s %+v", c.Status, c.Findings)
	}
}

func TestDeadcodeSummaries(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	t.Setenv("GOWORK", "off")
	dir, _ := filepath.Abs("testdata/dead2")
	r, err := Run(Options{Dir: dir, Only: []string{"deadcode"}, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range r.Checks[0].Findings {
		got = append(got, strings.TrimPrefix(f.Message, "unreachable func: "))
	}
	want := []string{"nested.unusedHelper", "hidden.String", "dead"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("dead: %v, want %v", got, want)
	}
}

func TestVulnOffline(t *testing.T) {
	t.Setenv("GOWORK", "off")
	dir, _ := filepath.Abs("testdata/dead")
	r, err := Run(Options{Dir: dir, Only: []string{"vuln"}, Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	if c := r.Checks[0]; c.Status != Skipped || !r.OK() {
		t.Errorf("offline vuln: %s, ok %v", c.Status, r.OK())
	}
}

// copyTree copies a test module into a temporary directory.
func copyTree(t *testing.T, src string) string {
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestFix(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	t.Setenv("GOWORK", "off")
	dir := copyTree(t, "testdata/mod")
	os.Remove(filepath.Join(dir, "sub", "w_windows.go"))
	cache := t.TempDir()
	res, err := Fix(FixOptions{Dir: dir, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range res.Applied {
		got = append(got, a.Check+" "+filepath.Base(a.File))
	}
	sort.Strings(got)
	if want := []string{"fmt b.go", "modernize a.go"}; !slices.Equal(got, want) {
		t.Errorf("applied %v, want %v", got, want)
	}
	if len(res.RolledBack) != 0 {
		t.Errorf("rolled back %v", res.RolledBack)
	}
	a, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	if !strings.Contains(string(a), "for i := range n {") {
		t.Errorf("a.go not modernized:\n%s", a)
	}
	// A second run finds nothing.
	res, err = Fix(FixOptions{Dir: dir, CacheDir: cache})
	if err != nil || len(res.Applied) != 0 {
		t.Errorf("second fix: %v %v", res.Applied, err)
	}
}

func TestFixDiffChangesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	t.Setenv("GOWORK", "off")
	dir := copyTree(t, "testdata/mod")
	os.Remove(filepath.Join(dir, "sub", "w_windows.go"))
	before, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	res, err := Fix(FixOptions{Dir: dir, CacheDir: t.TempDir(), Diff: true})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	if !bytes.Equal(before, after) || !strings.Contains(res.Diff, "+\tfor i := range n {") {
		t.Errorf("diff mode changed the file or printed no patch:\n%s", res.Diff)
	}
}

func TestRunChanged(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Setenv("GOWORK", "off")
	dir := copyTree(t, "testdata/mod")
	os.Remove(filepath.Join(dir, "sub", "w_windows.go"))
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Only a.go changes: sub/b.go (not formatted) is out of scope.
	f, _ := os.OpenFile(filepath.Join(dir, "a.go"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("\n// Edited.\nfunc Edited() {}\n")
	f.Close()
	r, err := Run(Options{Dir: dir, Changed: true, Base: "HEAD", Only: []string{"fmt", "vet"}, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	got := findings(r)
	if want := []string{"vet printf a.go:11"}; !slices.Equal(got, want) {
		t.Errorf("changed findings %v, want %v", got, want)
	}
}

func TestDeadcodeReusedAcrossLineShifts(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	t.Setenv("GOWORK", "off")
	dir := copyTree(t, "testdata/dead")
	cache := t.TempDir()
	opts := Options{Dir: dir, Only: []string{"deadcode"}, CacheDir: cache}
	r1, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.go")
	src, _ := os.ReadFile(main)
	// Two lines more above Dead: positions move, references do not.
	os.WriteFile(main, bytes.Replace(src, []byte("func used() {}"), []byte("func used() {\n\t// a note\n}"), 1), 0o644)
	r2, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	a, b := r1.Checks[0], r2.Checks[0]
	if !b.Cached || len(b.Findings) != 1 || b.Findings[0].Span.Line != a.Findings[0].Span.Line+2 {
		t.Errorf("deadcode after the edit: cached %v, %+v (before: line %d)", b.Cached, b.Findings, a.Findings[0].Span.Line)
	}
	// A new reference changes the shape: Dead is used now.
	src, _ = os.ReadFile(main)
	os.WriteFile(main, bytes.Replace(src, []byte("// a note"), []byte("Dead()"), 1), 0o644)
	r3, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	if c := r3.Checks[0]; c.Cached || len(c.Findings) != 0 {
		t.Errorf("deadcode after a new call: cached %v, %+v", c.Cached, c.Findings)
	}
}

func TestExhaustiveOptIn(t *testing.T) {
	if selected("exhaustive", nil, nil) {
		t.Error("exhaustive runs by default")
	}
	if !selected("vet", nil, nil) || !selected("exhaustive", map[string]bool{"exhaustive": true}, nil) {
		t.Error("vet by default, or exhaustive with -only")
	}
	lc := &lintConfig{}
	lc.Linters.Enable = []string{"exhaustive"}
	if !selected("exhaustive", nil, lc) {
		t.Error("exhaustive enabled in the lint configuration does not run")
	}
	lc.Linters.Enable, lc.Linters.Default, lc.Linters.Disable = nil, "all", []string{"exhaustive"}
	if selected("exhaustive", nil, lc) {
		t.Error("exhaustive disabled in the lint configuration runs")
	}
}

func TestFmtFiles(t *testing.T) {
	sc := &scope{dir: "/m", patterns: []string{"./cmd/...", "./internal/x"}}
	for p, want := range map[string]bool{
		"/m/cmd/a.go":          true,
		"/m/cmd/sub/b.go":      true,
		"/m/internal/x/c.go":   true,
		"/m/internal/x/y/d.go": false,
		"/m/backend/e.go":      false,
		"/m/cmd/testdata/f.go": false,
		"/m/cmd/_skip/g.go":    false,
		"/m/cmd/.hidden/h.go":  false,
	} {
		if got := sc.fmtFile(p); got != want {
			t.Errorf("fmtFile(%s) = %v, want %v", p, got, want)
		}
	}
	all := &scope{dir: "/m", patterns: []string{"./..."}}
	if !all.fmtFile("/m/a.go") || !all.fmtFile("/m/x/b.go") || all.fmtFile("/m/x/testdata/c.go") {
		t.Error("./... selects the module minus testdata")
	}
}

func TestFixLeavesConcurrentEdits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.go")
	os.WriteFile(p, []byte("someone else's edit"), 0o644)
	res := &FixResult{}
	res.writeFiles([]*fileState{{path: p, old: []byte("read by fix"), new: []byte("fixed")}}, true)
	if data, _ := os.ReadFile(p); string(data) != "someone else's edit" || len(res.Notes) != 1 {
		t.Errorf("a file changed after it was read was overwritten: %q %v", data, res.Notes)
	}
	start := time.Now().Add(-time.Hour)
	c := candidate{edits: []diag.Edit{{File: p}}}
	if kept := res.skipChanged([]candidate{c}, start); len(kept) != 0 {
		t.Error("the fix of a file modified after the analysis began was kept")
	}
}

func TestRunMemo(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	t.Setenv("GOWORK", "off")
	src, _ := filepath.Abs("testdata/dead")
	dir := t.TempDir()
	for _, n := range []string{"go.mod", "main.go"} {
		data, _ := os.ReadFile(filepath.Join(src, n))
		os.WriteFile(filepath.Join(dir, n), data, 0o644)
	}
	opts := Options{Dir: dir, Only: []string{"vet", "deadcode"}, CacheDir: t.TempDir()}
	a, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Run(opts)
	if !b.Checks[0].Cached || !slices.Equal(findings(a), findings(b)) {
		t.Errorf("unchanged tree: cached %v, %v then %v", b.Checks[0].Cached, findings(a), findings(b))
	}
	main := filepath.Join(dir, "main.go")
	data, _ := os.ReadFile(main)
	os.WriteFile(main, append(data, []byte("\nfunc Dead2() {}\n")...), 0o644)
	c, _ := Run(opts)
	if c.Checks[0].Cached && c.Checks[1].Cached || len(findings(c)) != len(findings(a))+1 {
		t.Errorf("after an edit: %v", findings(c))
	}
}

func TestWatchDiff(t *testing.T) {
	d := func(file, msg string, line int) diag.Diagnostic {
		return diag.Diagnostic{Check: "lint", Analyzer: "x", Message: msg, Span: diag.Span{File: file, Line: line}}
	}
	prev := findingSet(&Result{Checks: []*CheckResult{{Findings: []diag.Diagnostic{d("a.go", "one", 3), d("a.go", "two", 9), d("b.go", "one", 1)}}}})
	// An edit above moved "two"; "one" in a.go was fixed; b.go got a second "one".
	cur := findingSet(&Result{Checks: []*CheckResult{{Findings: []diag.Diagnostic{d("a.go", "two", 10), d("b.go", "one", 1), d("b.go", "one", 7)}}}})
	added, gone := diffFindings(prev, cur)
	if len(added) != 1 || added[0].Span.File != "b.go" || added[0].Span.Line != 7 {
		t.Errorf("added %+v", added)
	}
	if len(gone) != 1 || gone[0].Span.File != "a.go" || gone[0].Message != "one" {
		t.Errorf("gone %+v", gone)
	}
}

func TestStagedTree(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/st\n\ngo 1.26\n"), 0o644)
	main := filepath.Join(dir, "main.go")
	os.WriteFile(main, []byte("package main\n\nfunc main() {}\n\nfunc staged() {}\n"), 0o644)
	git("init", "-q")
	git("add", ".")
	// The working copy drops the dead function; the index keeps it.
	os.WriteFile(main, []byte("package main\n\nfunc main() {}\n"), 0o644)
	cache := t.TempDir()
	snap, root, err := stagedTree(dir, cache)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Run(Options{Dir: snap, Only: []string{"deadcode"}, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	mapPaths(r, snap, root)
	fs := r.Checks[0].Findings
	if len(fs) != 1 || fs[0].Span.File != main || !strings.Contains(fs[0].Message, "staged") {
		t.Fatalf("staged findings: %+v", fs)
	}
	w, _ := Run(Options{Dir: dir, Only: []string{"deadcode"}, CacheDir: cache})
	if n := len(w.Checks[0].Findings); n != 0 {
		t.Errorf("working tree findings: %d", n)
	}
	// Staging the edit updates the tree in place.
	git("add", "main.go")
	if _, _, err := stagedTree(dir, cache); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(snap, "main.go")); strings.Contains(string(data), "staged") {
		t.Error("the staged tree kept the old version")
	}
}

func TestWrapWords(t *testing.T) {
	got := wrapWords("modernizers newer than this go fix, not run: importcomment, reflecttypeassert, slicesclip", 40)
	want := []string{"modernizers newer than this go fix, not", "run: importcomment, reflecttypeassert,", "slicesclip"}
	if !slices.Equal(got, want) {
		t.Errorf("wrap: %q", got)
	}
	for _, l := range got {
		if utf8.RuneCountInString(l) > 40 {
			t.Errorf("line over 40 columns: %q", l)
		}
	}
}

func TestFindingLineGolden(t *testing.T) {
	dir := "/m"
	at := func(file string, line, col int) diag.Span {
		return diag.Span{File: dir + "/" + file, Line: line, Col: col}
	}
	for _, c := range []struct {
		d    diag.Diagnostic
		want string
	}{
		{diag.Diagnostic{Check: "fmt", Analyzer: "gofmt", Message: "file is not formatted", Span: at("main.go", 9, 1)},
			"./main.go:9:1: file is not formatted (gofmt)"},
		{diag.Diagnostic{Check: "fmt", Analyzer: "goimports", Message: "file is not formatted", Span: at("a/b.go", 3, 1)},
			"./a/b.go:3:1: file is not formatted (goimports)"},
		{diag.Diagnostic{Check: "lint", Analyzer: "errcheck", Message: "Error return value of `f.Close` is not checked", Span: at("x.go", 17, 9)},
			"./x.go:17:9: Error return value of `f.Close` is not checked (errcheck)"},
		{diag.Diagnostic{Check: "vet", Analyzer: "printf", Message: "fmt.Printf format %d has arg s of wrong type string (printf)", Span: at("x.go", 11, 2)},
			"./x.go:11:2: fmt.Printf format %d has arg s of wrong type string (printf)"},
		{diag.Diagnostic{Check: "tidy", Analyzer: "go mod tidy", Message: "go.mod and go.sum are not tidy", Span: at("go.mod", 1, 1)},
			"./go.mod:1:1: go.mod and go.sum are not tidy (go mod tidy)"},
	} {
		if got := findingLine(dir, &c.d); got != c.want {
			t.Errorf("findingLine:\n got %s\nwant %s", got, c.want)
		}
	}
	// The fmt check's own diagnostic, end to end.
	d := fmtDiag("/m/main.go", []byte("package main\nfunc f(){}\n"), []byte("package main\n\nfunc f() {}\n"))
	if got := findingLine(dir, &d); strings.Count(got, "(gofmt)") != 1 {
		t.Errorf("fmt finding names its formatter %d times: %s", strings.Count(got, "(gofmt)"), got)
	}
}

func TestFmtIgnoredDir(t *testing.T) {
	t.Setenv("GOWORK", "off")
	repo := t.TempDir()
	if d, err := filepath.EvalSymlinks(repo); err == nil {
		repo = d
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("scratch/\n"), 0o644)
	dir := filepath.Join(repo, "scratch")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/s\n\ngo 1.26\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main(){}\n"), 0o644)
	r, err := Run(Options{Dir: dir, Only: []string{"fmt"}, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(r.Checks[0].Findings); n != 1 {
		t.Errorf("fmt in a directory git ignores: %d findings, want 1", n)
	}
}

func TestFixLeavesCgoOutputAlone(t *testing.T) {
	if testing.Short() {
		t.Skip("runs cgo")
	}
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/c\n\ngo 1.26\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\n// static int add(int a, int b) { return a + b; }\nimport \"C\"\n\nfunc show(v interface{}) { println(v != nil) }\n\nfunc main() { show(int(C.add(1, 2))) }\n"), 0o644)
	r, err := Run(Options{Dir: dir, Only: []string{"modernize"}, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r.Checks[0].Findings {
		if filepath.Dir(d.Span.File) != dir {
			t.Errorf("finding outside the package: %s", d.Span.File)
		}
		if len(d.Fixes) > 0 {
			t.Errorf("a fix in a cgo package: %+v", d.Fixes[0])
		}
	}
	if n := len(r.Checks[0].Findings); n != 1 {
		t.Errorf("findings: %d, want the one in main.go", n)
	}
	if c := candidates(r, true); len(c) != 0 {
		t.Errorf("fix candidates in a cgo package: %d", len(c))
	}
	if editableFile(dir, filepath.Join(os.TempDir(), "x.go")) {
		t.Error("a file outside the module is editable")
	}
}

func TestInlineRespectsFileVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	t.Setenv("GOWORK", "off")
	inline := func(goVersion string) []string {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/v\n\ngo "+goVersion+"\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "v.go"), []byte("package v\n\nimport \"io/ioutil\"\n\nfunc D() (string, error) { return ioutil.TempDir(\"\", \"x\") }\n"), 0o644)
		r, err := Run(Options{Dir: dir, Only: []string{"modernize"}, CacheDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		var ms []string
		for _, d := range r.Checks[0].Findings {
			if d.Analyzer == "inline" {
				ms = append(ms, d.Message)
			}
		}
		return ms
	}
	// go fix inlines a call into a file only when the file's Go version is
	// at least the callee's, and the standard library's is the toolchain's.
	if ms := inline("1.15"); len(ms) != 1 || !strings.HasPrefix(ms[0], "cannot inline call to ioutil.TempDir (declared using go1.") {
		t.Errorf("go 1.15: %q", ms)
	}
	// A file at the toolchain's own version gets the fix.
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		t.Fatal(err)
	}
	v := strings.TrimPrefix(strings.TrimSpace(string(out)), "go")
	if i := strings.IndexAny(v, " -"); i >= 0 {
		v = v[:i]
	}
	if ms := inline(v); len(ms) != 1 || ms[0] != "Call of ioutil.TempDir should be inlined" {
		t.Errorf("go %s: %q", v, ms)
	}
}

func TestLintRelatedInformation(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the standard library")
	}
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/r\n\ngo 1.26\n"), 0o644)
	// S1034: the type switch could bind the value; staticcheck points at
	// the assertion inside the case as related information.
	os.WriteFile(filepath.Join(dir, "r.go"), []byte("package r\n\nfunc F(x any) int {\n\tswitch x.(type) {\n\tcase int:\n\t\treturn x.(int)\n\t}\n\treturn 0\n}\n"), 0o644)
	r, err := Run(Options{Dir: dir, Only: []string{"lint"}, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range r.Checks[0].Findings {
		got = append(got, fmt.Sprintf("%d %s %s", d.Span.Line, d.Analyzer, d.Message))
	}
	if len(got) != 2 || !strings.HasPrefix(got[1], "6 S1034 related information: ") {
		t.Errorf("S1034 and its related location: %q", got)
	}
}
