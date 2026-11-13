package check

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"golang.org/x/vuln/scan"
)

type vulnFinding struct {
	OSV, Summary, Fixed string
	Module, Version     string
	Function            string // the vulnerable symbol, "" when only the module is affected
	Span                diag.Span
	Trace               []string
}

type vulnResult struct {
	Findings []vulnFinding
	Modules  int // vulnerable modules required but not called
	// Imported counts vulnerable packages the code imports. When it is
	// zero, no edit of a function body can reach a vulnerable symbol, so
	// the result holds while go.mod, go.sum and the imports stay the same.
	Imported int
	Err      string
}

// runVuln runs govulncheck in process for every module of the scope (in
// Changed mode, the modules whose files changed) and reports the
// vulnerabilities its call graph reaches. Results are cached per module
// for a day: the database changes, the code is keyed by its files.
func runVuln(sc *scope, opts Options, st *store, r *CheckResult) {
	if opts.Offline {
		r.Status = Skipped
		r.Notes = append(r.Notes, "offline: the vulnerability database was not read")
		return
	}
	// The database is fetched per module: run a few modules at once.
	results := make([]vulnResult, len(sc.modules))
	skip := make([]bool, len(sc.modules))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, m := range sc.modules {
		if sc.changed != nil && !sc.moduleTouched(&m) {
			skip[i] = true
			continue
		}
		wg.Add(1)
		go func(i int, m module) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = vulnModule(st, m, opts.Tags)
		}(i, m)
	}
	wg.Wait()
	for i, m := range sc.modules {
		if skip[i] {
			continue
		}
		res := results[i]
		rel := diag.RelPath(sc.dir, m.Dir)
		if m.Dir == sc.dir {
			rel = "."
		}
		if res.Err != "" {
			if r.Status != Failed {
				r.Status = Skipped
			}
			r.Notes = append(r.Notes, rel+": "+res.Err)
			continue
		}
		if res.Modules > 0 {
			r.Notes = append(r.Notes, fmt.Sprintf("%s: %d vulnerabilities in required modules that the code does not call", rel, res.Modules))
		}
		for _, f := range res.Findings {
			d := diag.Diagnostic{
				Kind: "diagnostic", Severity: diag.Error, Code: diag.CodeVuln, Check: "vuln", Analyzer: f.OSV,
				Message: fmt.Sprintf("%s (%s@%s, called: %s)", f.Summary, f.Module, f.Version, f.Function),
				Span:    f.Span,
				Notes:   f.Trace,
				DocURL:  "https://pkg.go.dev/vuln/" + f.OSV,
			}
			if d.Span.Valid() && !filepath.IsAbs(d.Span.File) {
				d.Span.File = filepath.Join(m.Dir, d.Span.File)
			}
			if !d.Span.Valid() {
				d.Span = diag.Span{File: filepath.Join(m.Dir, "go.mod"), Line: 1, Col: 1, EndLine: 1, EndCol: 1}
			}
			if f.Fixed != "" {
				d.Help = append(d.Help, "upgrade "+f.Module+" to "+f.Fixed)
			} else {
				d.Help = append(d.Help, "no fixed version yet")
			}
			r.Findings = append(r.Findings, d)
		}
	}
}

// vulnModule returns the cached or fresh result for one module.
func vulnModule(st *store, m module, tags []string) vulnResult {
	h := newHasher("vuln")
	h.str(toolID(), envKey(tags, hostGOOS(), hostGOARCH()), m.Dir, time.Now().UTC().Format("2006-01-02"))
	var files []string
	walkModule(m.Dir, func(p string, info os.FileInfo) {
		files = append(files, fmt.Sprintf("%s %d %d", p, info.Size(), info.ModTime().UnixNano()))
	})
	sort.Strings(files)
	h.str(files...)
	key := h.sum()
	// The imports key: go.mod, go.sum and every file's imports.
	ih := newHasher("vuln-imports")
	ih.str(toolID(), envKey(tags, hostGOOS(), hostGOARCH()), m.Dir, time.Now().UTC().Format("2006-01-02"))
	var heads []string
	walkModule(m.Dir, func(p string, info os.FileInfo) {
		heads = append(heads, p+" "+headHash(p))
	})
	sort.Strings(heads)
	ih.str(heads...)
	ikey := ih.sum()
	var res vulnResult
	if c := st.get(key); c != nil && gob.NewDecoder(bytes.NewReader(c)).Decode(&res) == nil {
		return res
	}
	if c := st.get(ikey); c != nil && gob.NewDecoder(bytes.NewReader(c)).Decode(&res) == nil && res.Imported == 0 {
		return res
	}
	res = govulncheck(m.Dir, tags)
	if res.Err == "" {
		var buf bytes.Buffer
		if gob.NewEncoder(&buf).Encode(res) == nil {
			st.put(key, buf.Bytes())
			st.put(ikey, buf.Bytes())
		}
	}
	return res
}

// govulncheck runs golang.org/x/vuln/scan with JSON output and reads the
// symbol level findings.
func govulncheck(dir string, tags []string) vulnResult {
	args := []string{"-C", dir, "-format", "json"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, "./...")
	var out, errb bytes.Buffer
	cmd := scan.Command(context.Background(), args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Start(); err == nil {
		err = cmd.Wait()
	}
	type frame struct {
		Module, Version, Package, Function, Receiver string
		Position                                     *struct {
			Filename     string
			Line, Column int
		}
	}
	type msg struct {
		OSV *struct {
			ID, Summary string
		} `json:"osv"`
		Finding *struct {
			OSV          string  `json:"osv"`
			FixedVersion string  `json:"fixed_version"`
			Trace        []frame `json:"trace"`
		} `json:"finding"`
	}
	summaries := map[string]string{}
	var res vulnResult
	modOnly := map[string]bool{}
	pkgLevel := map[string]bool{}
	seen := map[string]bool{}
	dec := json.NewDecoder(&out)
	for {
		var m msg
		if err := dec.Decode(&m); err != nil {
			if err != io.EOF && len(res.Findings) == 0 {
				res.Err = "govulncheck: " + firstLine(strings.TrimSpace(errb.String()+" "+err.Error()))
			}
			break
		}
		if m.OSV != nil {
			summaries[m.OSV.ID] = m.OSV.Summary
		}
		f := m.Finding
		if f == nil || len(f.Trace) == 0 {
			continue
		}
		top := f.Trace[0]
		if top.Function == "" {
			if top.Package == "" {
				modOnly[f.OSV] = true
			} else {
				pkgLevel[f.OSV] = true
			}
			continue
		}
		vf := vulnFinding{OSV: f.OSV, Fixed: f.FixedVersion, Module: top.Module, Version: top.Version, Function: qualify(top.Package, top.Receiver, top.Function)}
		for _, fr := range f.Trace {
			s := qualify(fr.Package, fr.Receiver, fr.Function)
			if fr.Position != nil && fr.Position.Line > 0 {
				s += fmt.Sprintf(" at %s:%d", fr.Position.Filename, fr.Position.Line)
				vf.Span = diag.Span{File: fr.Position.Filename, Line: fr.Position.Line, Col: fr.Position.Column, EndLine: fr.Position.Line, EndCol: fr.Position.Column}
			}
			vf.Trace = append(vf.Trace, s)
		}
		k := vf.OSV + vf.Function + fmt.Sprint(vf.Span)
		if !seen[k] {
			seen[k] = true
			res.Findings = append(res.Findings, vf)
		}
	}
	for i := range res.Findings {
		res.Findings[i].Summary = summaries[res.Findings[i].OSV]
		delete(modOnly, res.Findings[i].OSV)
	}
	for o := range pkgLevel {
		delete(modOnly, o)
	}
	res.Modules = len(modOnly)
	res.Imported = len(pkgLevel) + len(res.Findings)
	sort.Slice(res.Findings, func(i, j int) bool {
		a, b := res.Findings[i], res.Findings[j]
		if a.OSV != b.OSV {
			return a.OSV < b.OSV
		}
		return a.Span.File < b.Span.File || a.Span.File == b.Span.File && a.Span.Line < b.Span.Line
	})
	if res.Err == "" && errb.Len() > 0 && strings.Contains(errb.String(), "fetching vulnerabilities") {
		res.Err = "govulncheck: " + firstLine(errb.String())
	}
	return res
}

func qualify(pkg, recv, fn string) string {
	s := pkg
	if recv != "" {
		s += "." + strings.TrimPrefix(recv, "*")
	}
	if fn != "" {
		s += "." + fn
	}
	return s
}
