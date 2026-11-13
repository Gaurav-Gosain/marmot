package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/Gaurav-Gosain/marmot/internal/exhaustive"
	"github.com/Gaurav-Gosain/marmot/internal/nilcheck"
	"golang.org/x/tools/go/analysis"
)

// toolID identifies the running marmot binary, so a rebuilt marmot (with
// changed analyzers) never reuses results of an older one.
var toolID = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	st, err := os.Stat(exe)
	if err != nil {
		return exe
	}
	return fmt.Sprintf("%s|%d|%d", exe, st.Size(), st.ModTime().UnixNano())
})

// runAnalysis runs vet, lint, modernize and gopls on one package graph.
func runAnalysis(sc *scope, opts Options, st *store, lc *lintConfig, lcErr error, results map[string]*CheckResult, stats *Stats) {
	fams := map[string]*family{}
	var all []*analysis.Analyzer
	moduleGo := ""
	if len(sc.modules) > 0 {
		moduleGo = sc.modules[0].Go
	}
	if r := results["vet"]; r != nil {
		f := vetFamily()
		if d := f.restrict(toolchainAnalyzers(sc.dir)); len(d) > 0 {
			r.Notes = append(r.Notes, "analyzers newer than this go vet, not run: "+strings.Join(d, ", "))
		}
		fams["vet"] = f
	}
	if r := results["lint"]; r != nil {
		if lcErr != nil {
			r.Status = Failed
			r.Notes = append(r.Notes, lcErr.Error())
		} else {
			f := lintFamily(lc, moduleGo)
			fams["lint"] = f
			r.Notes = append(r.Notes, f.notes...)
			if lc != nil {
				r.Notes = append(r.Notes, "configuration: "+diag.RelPath(sc.dir, lc.path))
			}
		}
	}
	if r := results["modernize"]; r != nil {
		f := modernizeFamily()
		if d := f.restrict(toolchainAnalyzers(sc.dir)); len(d) > 0 {
			r.Notes = append(r.Notes, "modernizers newer than this go fix, not run: "+strings.Join(d, ", "))
		}
		fams["modernize"] = f
	}
	if r := results["gopls"]; r != nil {
		r.Status = Skipped
		r.Notes = append(r.Notes, "the gopls only analyzers are not in marmot check yet; vet, lint and modernize cover the rest of gopls check")
	}
	if results["exhaustive"] != nil {
		fams["exhaustive"] = &family{}
		fams["exhaustive"].add("exhaustive", exhaustive.Analyzer)
	}
	if results["nil"] != nil {
		fams["nil"] = &family{}
		fams["nil"].add("nil", nilcheck.Analyzer)
	}
	for _, n := range analysisChecks {
		if f := fams[n]; f != nil {
			all = append(all, f.analyzers...)
		}
	}
	if len(all) == 0 {
		return
	}
	if len(sc.patterns) == 0 {
		return // nothing changed
	}
	tags := opts.Tags
	if lc != nil {
		tags = append(tags, lc.Run.BuildTags...)
	}
	cfgHash := ""
	if lc != nil {
		cfgHash = fileHash(lc.path)
	}
	s := newSuite(all, toolID(), cfgHash)

	t0 := time.Now()
	opts.progress("check: loading packages")
	g, _, err := cachedGoList(st, sc, tags, nil, true)
	if err != nil {
		for n := range fams {
			results[n].Status = Failed
			results[n].Notes = append(results[n].Notes, err.Error())
		}
		return
	}
	stats.ListTime = time.Since(t0)
	stats.Packages, stats.Roots = len(g.pkgs), len(g.roots)
	e := newEngine(g, st, s, envKey(tags, hostGOOS(), hostGOARCH()), hostGOARCH(), opts.Jobs)
	opts.progress("check: analyzing %d packages (%d with their dependencies)", len(g.roots), len(g.pkgs))
	t1 := time.Now()
	reps := make([]*report, len(g.roots))
	errs := make([]error, len(g.roots))
	var wg sync.WaitGroup
	for i, p := range g.roots {
		wg.Add(1)
		go func(i int, p *listPkg) {
			defer wg.Done()
			reps[i], errs[i] = e.report(p)
		}(i, p)
	}
	wg.Wait()
	stats.AnalyzeTime = time.Since(t1)
	stats.Hits, stats.Misses, stats.Analyzed = e.hits, e.misses, e.analyzed
	sort.Slice(e.slow, func(i, j int) bool { return e.slow[i].Dur > e.slow[j].Dur })
	for i, s := range e.slow {
		if i == 25 {
			break
		}
		opts.progress("check: slow package %s %.2fs", s.ID, s.Dur.Seconds())
	}

	// Type and load errors stop go vet; report them once, in the first
	// analysis check that runs.
	errCheck := ""
	for _, n := range analysisChecks {
		if fams[n] != nil {
			errCheck = n
			break
		}
	}
	seen := map[string]bool{}
	for i, p := range g.roots {
		if errs[i] != nil {
			r := results[errCheck]
			r.Status = Failed
			r.Notes = append(r.Notes, fmt.Sprintf("%s: %v", p.ImportPath, firstLine(errs[i].Error())))
			continue
		}
		rep := reps[i]
		for _, le := range p.listErrors() {
			d := diagFromText(le, sc.dir)
			d.Check = errCheck
			addOnce(results[errCheck], d, seen)
			results[errCheck].Status = Failed
		}
		for _, te := range rep.TypeErrors {
			d := diag.Diagnostic{Kind: "diagnostic", Severity: diag.Error, Code: diag.Classify(te.Message), Message: te.Message, Span: te.Span, Check: errCheck, Analyzer: "typecheck", Package: p.path, Related: te.Related}
			addOnce(results[errCheck], d, seen)
			results[errCheck].Status = Failed
		}
		for _, msg := range rep.Errors {
			for n := range fams {
				results[n].Notes = append(results[n].Notes, "analyzer failed: "+msg)
			}
		}
		for _, n := range analysisChecks {
			f := fams[n]
			if f == nil {
				continue
			}
			type lf struct {
				fd     finding
				linter string
			}
			var fs []lf
			for _, fd := range rep.Findings {
				for _, a := range f.analyzers {
					if a.Name == fd.Analyzer {
						fs = append(fs, lf{fd, f.linter[a]})
						break
					}
				}
			}
			if n == "lint" {
				// golangci-lint keeps one issue per line, and sees the
				// issues grouped by linter in name order.
				sort.SliceStable(fs, func(i, j int) bool { return fs[i].linter < fs[j].linter })
			}
			lines := map[string]bool{}
			for _, x := range fs {
				if n == "lint" && lintFiltered(lc, sc, rep, x.fd, x.linter, lines) {
					continue
				}
				addOnce(results[n], toDiag(n, x.linter, x.fd, p.path), seen)
			}
		}
	}
}

// analysisChecks are the checks that run analyzers on the package graph,
// in reporting order.
var analysisChecks = []string{"vet", "lint", "modernize", "exhaustive", "nil"}

// lintFiltered applies golangci-lint's filters: generated files, test
// files when tests are off, exclusion rules, //nolint and one issue per
// line per linter.
func lintFiltered(lc *lintConfig, sc *scope, rep *report, fd finding, linter string, lines map[string]bool) bool {
	file := fd.Span.File
	if lc == nil || lc.Linters.Exclusions.Generated != "disable" {
		for _, g := range rep.Generated {
			if g == file {
				return true
			}
		}
	}
	if lc != nil && lc.Run.Tests != nil && !*lc.Run.Tests && strings.HasSuffix(file, "_test.go") {
		return true
	}
	if suppressed(rep.Nolint, file, fd.Span.Line, linter, fd.Analyzer) {
		return true
	}
	if lc != nil {
		rel, err := filepath.Rel(lc.dir, file)
		if err != nil {
			rel = file
		}
		text := golangciText(linter, fd)
		if lc.excluded(linter, filepath.ToSlash(rel), text, sourceLine(file, fd.Span.Line)) {
			return true
		}
	}
	// One issue per line, across linters, the leftmost first (findings
	// are sorted by position).
	k := file + ":" + strconv.Itoa(fd.Span.Line)
	if lines[k] {
		return true
	}
	lines[k] = true
	return false
}

// golangciText is the issue text golangci-lint matches exclusion rules
// against: the analyzer name first for linters made of several.
func golangciText(linter string, fd finding) string {
	if fd.Analyzer != linter {
		return fd.Analyzer + ": " + fd.Message
	}
	return fd.Message
}

var srcCache sync.Map // file -> []string

func sourceLine(file string, line int) string {
	v, ok := srcCache.Load(file)
	if !ok {
		data, _ := os.ReadFile(file)
		v, _ = srcCache.LoadOrStore(file, strings.Split(string(data), "\n"))
	}
	ls := v.([]string)
	if line < 1 || line > len(ls) {
		return ""
	}
	return ls[line-1]
}

func addOnce(r *CheckResult, d diag.Diagnostic, seen map[string]bool) {
	k := fmt.Sprintf("%s|%s|%s:%d:%d|%s", d.Check, d.Analyzer, d.Span.File, d.Span.Line, d.Span.Col, d.Message)
	if seen[k] {
		return
	}
	seen[k] = true
	r.Findings = append(r.Findings, d)
}

// machineFixes are the analyzers whose suggested fixes are safe to apply
// without looking (they are what go fix applies).
func machineApplicable(check string) bool { return check == "modernize" || check == "fmt" }

func toDiag(check, linter string, fd finding, pkg string) diag.Diagnostic {
	d := diag.Diagnostic{
		Kind:     "diagnostic",
		Severity: diag.Warning,
		Code:     checkCode(check),
		Message:  fd.Message,
		Span:     fd.Span,
		Check:    check,
		Analyzer: fd.Analyzer,
		DocURL:   fd.URL,
		Package:  pkg,
		Related:  fd.Related,
	}
	if check == "lint" && linter != fd.Analyzer {
		d.Notes = append(d.Notes, "linter "+linter)
	}
	app := diag.MaybeIncorrect
	if machineApplicable(check) {
		app = diag.MachineApplicable
	}
	for _, f := range fd.Fixes {
		d.Fixes = append(d.Fixes, diag.Fix{Title: f.Message, Applicability: app, Edits: f.Edits})
	}
	return d
}

func checkCode(check string) string {
	switch check {
	case "vet":
		return diag.CodeVet
	case "lint":
		return diag.CodeLint
	case "modernize":
		return diag.CodeModernize
	case "fmt":
		return diag.CodeNotFmt
	case "gopls":
		return diag.CodeGopls
	case "vuln":
		return diag.CodeVuln
	case "tidy":
		return diag.CodeNotTidy
	case "cross":
		return diag.CodeCross
	case "deadcode":
		return diag.CodeDeadcode
	case "exhaustive":
		return diag.CodeExhaustive
	case "nil":
		return diag.CodeNil
	}
	return diag.CodeOther
}

var rePosPrefix = regexp.MustCompile(`^(.+?\.go):(\d+)(?::(\d+))?: (.*)$`)

// diagFromText makes an error diagnostic from a "file:line:col: msg" line.
func diagFromText(text, dir string) diag.Diagnostic {
	d := diag.Diagnostic{Kind: "diagnostic", Severity: diag.Error, Message: text}
	if m := rePosPrefix.FindStringSubmatch(text); m != nil {
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		f := m[1]
		if !filepath.IsAbs(f) {
			f = filepath.Join(dir, f)
		}
		d.Span = diag.Span{File: f, Line: line, Col: col, EndLine: line, EndCol: col}
		d.Message = m[4]
	}
	d.Code = diag.Classify(d.Message)
	return d
}
