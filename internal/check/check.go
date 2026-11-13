// Package check runs the checks people run by hand or in CI (gofmt and
// goimports, go vet, staticcheck and the golangci-lint standard set, the
// go fix modernizers, govulncheck, go mod tidy, cross builds and dead code)
// in one process, on one package graph, with every result cached per
// package by a key that covers all of its inputs. An unchanged tree is
// answered from the cache; after an edit only the packages whose inputs
// changed are analyzed again.
package check

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/checkflags"
	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

// Names are the checks, in the order they run and are reported.
var Names = checkflags.Names

// reportOnly checks print their findings but never fail the run, as the
// modernizers and dead code are in most CI gates.
var reportOnly = map[string]bool{"modernize": true, "deadcode": true, "exhaustive": true, "nil": true}

// Codes of check findings, one per check.
var Codes = map[string]string{
	"vet":       "M0100",
	"lint":      "M0101",
	"modernize": "M0102",
	"fmt":       "M0103",
	"gopls":     "M0104",
	"vuln":      "M0105",
	"tidy":      "M0106",
	"cross":     "M0107",
	"deadcode":  "M0108",
	"typecheck": "M0109",
}

// Options configure a run.
type Options struct {
	Dir      string   // the directory to check (default: the current one)
	Patterns []string // package patterns; default every module of the workspace
	Only     []string // check names; default all
	Changed  bool     // only what changed against Base
	Base     string   // git ref for Changed (default origin/main, else main)
	Tags     []string // build tags
	Jobs     int      // parallel analysis jobs (default GOMAXPROCS)
	CacheDir string   // "" disables the cache
	Targets  []string // cross build targets (goos/goarch); default the platforms file
	Offline  bool     // no network (vuln is skipped)
	Progress io.Writer
}

// Status of one check.
const (
	Pass     = "pass"
	Findings = "findings" // a report-only check found something
	Failed   = "failed"
	Skipped  = "skip"
)

// CheckResult is the outcome of one check.
type CheckResult struct {
	Name     string            `json:"check"`
	Status   string            `json:"status"`
	Count    int               `json:"findings"`
	Gate     bool              `json:"gate"`
	Notes    []string          `json:"notes,omitempty"`
	Findings []diag.Diagnostic `json:"-"`
	Duration time.Duration     `json:"-"`
	Cached   bool              `json:"cached,omitempty"`
}

// Result is the outcome of a run.
type Result struct {
	Dir    string
	Checks []*CheckResult
	Stats  Stats
}

// Stats describe how much work a run did (not part of the deterministic
// output).
type Stats struct {
	Packages, Roots        int
	Hits, Misses, Analyzed int
	ListTime, AnalyzeTime  time.Duration
}

// OK reports whether every gating check passed.
func (r *Result) OK() bool {
	for _, c := range r.Checks {
		if c.Status == Failed {
			return false
		}
	}
	return true
}

// scope is what a run looks at.
type scope struct {
	dir       string   // absolute
	modules   []module // the workspace modules
	patterns  []string
	changed   map[string]bool // absolute paths of changed files (Changed mode)
	changedGo []string
	base      string
}

type module struct {
	Dir  string // absolute
	Path string
	Go   string
}

// Run runs the checks.
func Run(opts Options) (*Result, error) {
	dir := opts.Dir
	if dir == "" {
		dir = "."
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	// The go command reports module directories with symbolic links
	// resolved (on macOS /var is /private/var).
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	sc, err := makeScope(dir, opts)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, n := range opts.Only {
		found := false
		for _, k := range Names {
			if k == n {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown check %q (checks: %s)", n, strings.Join(Names, ", "))
		}
		want[n] = true
	}
	on := func(n string) bool { return len(want) == 0 || want[n] }
	res := &Result{Dir: dir}
	results := map[string]*CheckResult{}
	for _, n := range Names {
		if on(n) {
			results[n] = &CheckResult{Name: n, Gate: !reportOnly[n]}
		}
	}
	st := openStore(opts.CacheDir)
	lc, lcErr := findLintConfig(dir)

	var wg sync.WaitGroup
	run := func(name string, f func(*CheckResult)) {
		r := results[name]
		if r == nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			t0 := time.Now()
			f(r)
			r.Duration = time.Since(t0)
		}()
	}
	// The analysis checks share one package graph and one pass.
	analysis := []string{}
	for _, n := range []string{"vet", "lint", "modernize", "gopls", "exhaustive", "nil"} {
		if results[n] != nil {
			analysis = append(analysis, n)
		}
	}
	if len(analysis) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t0 := time.Now()
			runAnalysis(sc, opts, st, lc, lcErr, results, &res.Stats)
			for _, n := range analysis {
				results[n].Duration = time.Since(t0)
			}
		}()
	}
	run("fmt", func(r *CheckResult) { runFmt(sc, st, r) })
	run("tidy", func(r *CheckResult) { runTidy(sc, st, r) })
	wg.Wait()
	// Cross builds, dead code and vulnerabilities load whole programs; run
	// them after the analysis so they do not compete for memory.
	for _, n := range []string{"cross", "deadcode", "vuln"} {
		run(n, func(r *CheckResult) {
			switch n {
			case "cross":
				runCross(sc, opts, st, r)
			case "deadcode":
				runDeadcode(sc, opts, st, r)
			case "vuln":
				runVuln(sc, opts, st, r)
			}
		})
		wg.Wait()
	}
	for _, n := range Names {
		if r := results[n]; r != nil {
			finish(r)
			res.Checks = append(res.Checks, r)
		}
	}
	return res, nil
}

// finish sorts a check's findings and sets its status.
func finish(r *CheckResult) {
	diag.Sort(r.Findings)
	r.Count = len(r.Findings)
	sort.Strings(r.Notes)
	r.Notes = dedupStrings(r.Notes)
	switch {
	case r.Status == Skipped || r.Status == Failed:
	case r.Count == 0:
		r.Status = Pass
	case r.Gate:
		r.Status = Failed
	default:
		r.Status = Findings
	}
}

func dedupStrings(s []string) []string {
	out := s[:0]
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// makeScope finds the modules and, in Changed mode, the changed files.
func makeScope(dir string, opts Options) (*scope, error) {
	sc := &scope{dir: dir}
	mods, err := workspaceModules(dir)
	if err != nil {
		return nil, err
	}
	sc.modules = mods
	if len(opts.Patterns) > 0 {
		sc.patterns = opts.Patterns
	} else {
		for _, m := range mods {
			rel, err := filepath.Rel(dir, m.Dir)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			if rel == "." {
				sc.patterns = append(sc.patterns, "./...")
			} else {
				sc.patterns = append(sc.patterns, "./"+filepath.ToSlash(rel)+"/...")
			}
		}
	}
	if !opts.Changed {
		return sc, nil
	}
	base := opts.Base
	if base == "" {
		base = "origin/main"
		if gitOut(dir, "rev-parse", "--verify", "-q", base) == "" {
			base = "main"
		}
	}
	mb := gitOut(dir, "merge-base", base, "HEAD")
	if mb == "" {
		return nil, fmt.Errorf("no merge base with %s: run git fetch, or name a base with -changed=REF", base)
	}
	sc.base = base
	top := gitOut(dir, "rev-parse", "--show-toplevel")
	names := strings.Fields(gitOut(dir, "diff", "--name-only", "--diff-filter=d", mb))
	names = append(names, strings.Fields(gitOut(dir, "ls-files", "--others", "--exclude-standard", "--full-name"))...)
	sc.changed = map[string]bool{}
	dirs := map[string]bool{}
	for _, n := range names {
		p := filepath.Join(top, n)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		sc.changed[p] = true
		if strings.HasSuffix(p, ".go") {
			sc.changedGo = append(sc.changedGo, p)
			d := filepath.Dir(p)
			if strings.Contains(filepath.ToSlash(d)+"/", "/testdata/") {
				continue
			}
			dirs[d] = true
		}
	}
	sort.Strings(sc.changedGo)
	sc.patterns = nil
	for d := range dirs {
		rel, err := filepath.Rel(dir, d)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		sc.patterns = append(sc.patterns, "./"+filepath.ToSlash(rel))
	}
	sort.Strings(sc.patterns)
	return sc, nil
}

// workspaceModules returns the modules of the go.work that covers dir, or
// the one module of dir.
func workspaceModules(dir string) ([]module, error) {
	out, err := goCmd(dir, nil, "env", "-json", "GOWORK", "GOMOD")
	if err != nil {
		return nil, err
	}
	var env struct{ GOWORK, GOMOD string }
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		return nil, err
	}
	var dirs []string
	if env.GOWORK != "" && env.GOWORK != "off" {
		w, err := goCmd(dir, nil, "work", "edit", "-json", env.GOWORK)
		if err != nil {
			return nil, err
		}
		var wf struct{ Use []struct{ DiskPath string } }
		if err := json.Unmarshal([]byte(w), &wf); err != nil {
			return nil, err
		}
		for _, u := range wf.Use {
			dirs = append(dirs, filepath.Join(filepath.Dir(env.GOWORK), u.DiskPath))
		}
	} else if env.GOMOD != "" && env.GOMOD != os.DevNull {
		dirs = append(dirs, filepath.Dir(env.GOMOD))
	} else {
		return nil, fmt.Errorf("%s is not in a Go module", dir)
	}
	var mods []module
	for _, d := range dirs {
		m := module{Dir: d}
		if j, err := goCmd(d, []string{"GOWORK=off"}, "mod", "edit", "-json"); err == nil {
			var mf struct {
				Module struct{ Path string }
				Go     string
			}
			if json.Unmarshal([]byte(j), &mf) == nil {
				m.Path, m.Go = mf.Module.Path, mf.Go
			}
		}
		mods = append(mods, m)
	}
	return mods, nil
}

func goCmd(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("go %s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

func gitOut(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// moduleOf returns the module that holds a file, the innermost one.
func (sc *scope) moduleOf(path string) *module {
	var best *module
	for i := range sc.modules {
		m := &sc.modules[i]
		if strings.HasPrefix(path, m.Dir+string(os.PathSeparator)) || path == m.Dir {
			if best == nil || len(m.Dir) > len(best.Dir) {
				best = m
			}
		}
	}
	return best
}

// envKey is the part of every cache key that the build configuration
// sets.
func envKey(tags []string, goos, goarch string) string {
	return strings.Join([]string{goos, goarch, strings.Join(tags, ","), os.Getenv("GOFLAGS"), os.Getenv("CGO_ENABLED"), os.Getenv("GOEXPERIMENT"), runtime.Version()}, "|")
}

func hostGOOS() string   { return goenv("GOOS", runtime.GOOS) }
func hostGOARCH() string { return goenv("GOARCH", runtime.GOARCH) }

func goenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (o Options) progress(format string, args ...any) {
	if o.Progress != nil {
		fmt.Fprintf(o.Progress, format+"\n", args...)
	}
}
