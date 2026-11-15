package check

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/checkflags"
	"github.com/Gaurav-Gosain/marmot/internal/diag"
	vfy "github.com/Gaurav-Gosain/marmot/internal/verify"
)

// verifyDiag finishes a verify finding: the statistics diagnostic is
// summed into r and dropped; a finding gets its verify object and is a
// note (no class is an error until its false report sample passes);
// latent findings are dropped unless asked for.
func verifyDiag(r *CheckResult, d *diag.Diagnostic, fd finding, latent bool) bool {
	kind, payload := vfy.DecodeCategory(fd.Category)
	switch kind {
	case vfy.CategoryStats:
		var s vfy.Summary
		if json.Unmarshal(payload, &s) == nil {
			if r.Verify == nil {
				r.Verify = &vfy.Summary{}
			}
			r.Verify.Add(s)
		}
		return false
	case vfy.CategorySearch:
		var h vfy.Hint
		if json.Unmarshal(payload, &h) == nil {
			r.search = append(r.search, searchHint{pkg: d.Package, dir: filepath.Dir(d.Span.File), hint: &h})
		}
		return false
	case vfy.CategoryFinding:
		var in vfy.Info
		if err := json.Unmarshal(payload, &in); err != nil {
			return false
		}
		if in.Class != "manifest" && !latent {
			return false
		}
		d.Verify = payload
		d.Severity = diag.Note
		d.Primary = in.Label
		// The guard and the call are labels in the snippet when they are
		// in the finding's file.
		var rel []diag.Related
		for _, r := range d.Related {
			if r.Span.File == d.Span.File {
				d.Labels = append(d.Labels, diag.Label{Span: r.Span, Text: r.Message})
			} else {
				rel = append(rel, r)
			}
		}
		d.Related = rel
		return true
	}
	return true
}

// searchHint is one function the search can call, with its package.
type searchHint struct {
	pkg, dir string
	hint     *vfy.Hint
}

// verifySearch runs the directed search: on every hint with search, else
// only on the functions of manifest findings (to confirm them). It
// updates the findings it confirms and adds one for every other panic it
// confirms; the added ones are shown with -latent or -search.
func verifySearch(c *CheckResult, search, show, effects bool, progress func(string)) []string {
	var notes []string
	type key struct{ fn, obl string }
	byKey := map[key]int{}
	wantFn := map[string]bool{}
	for i := range c.Findings {
		var in vfy.Info
		if json.Unmarshal(c.Findings[i].Verify, &in) != nil {
			continue
		}
		byKey[key{in.Fn, in.Obligation}] = i
		if in.Class == "manifest" {
			wantFn[in.Fn] = true
		}
	}
	byDir := map[string][]searchHint{}
	var dirs []string
	for _, h := range c.search {
		if !search && !wantFn[h.hint.Full] {
			continue
		}
		if _, ok := byDir[h.dir]; !ok {
			dirs = append(dirs, h.dir)
		}
		byDir[h.dir] = append(byDir[h.dir], h)
	}
	slices.Sort(dirs)
	for _, dir := range dirs {
		hs := byDir[dir]
		var hints []*vfy.Hint
		for _, h := range hs {
			hints = append(hints, h.hint)
		}
		slices.SortFunc(hints, func(a, b *vfy.Hint) int { return strings.Compare(a.Full, b.Full) })
		hits, err := vfy.Search(context.Background(), dir, hints[0].PkgName, hints, vfy.SearchOptions{Progress: progress, Effects: effects})
		if err != nil {
			notes = append(notes, fmt.Sprintf("search in %s: %v", hs[0].pkg, err))
			continue
		}
		obls := map[key]vfy.OblRef{}
		for _, h := range hints {
			for _, o := range h.Obls {
				obls[key{h.Full, o.Key}] = o
			}
		}
		for _, hit := range hits {
			k := key{hit.Fn, hit.Obligation}
			confirmed := "function-level"
			if hit.API {
				confirmed = "api"
			}
			if hit.Runs < 3 {
				confirmed = "none"
			}
			call := hit.Call
			note := fmt.Sprintf("confirmed under gc: %s panics: %s", call, hit.PanicMessage)
			if hit.Runs < 3 {
				note = fmt.Sprintf("flaky: %s panicked in %d of 3 runs: %s", call, hit.Runs, hit.PanicMessage)
			}
			if i, ok := byKey[k]; ok {
				d := &c.Findings[i]
				var in vfy.Info
				_ = json.Unmarshal(d.Verify, &in)
				in.Confirmed, in.Input, in.PanicMessage = confirmed, hit.Input, hit.PanicMessage
				if hit.Runs == 3 {
					in.Status = "counterexample"
				}
				if hit.Runs == 3 {
					in.ReproducedBy = []string{"gc"}
				}
				in.Engine = "pentagon+search"
				d.Verify, _ = json.Marshal(in)
				d.Notes = append(d.Notes, note)
				continue
			}
			o := obls[k]
			if c.Verify == nil {
				c.Verify = &vfy.Summary{}
			}
			c.Verify.Counterexamples++
			if !show {
				continue
			}
			in := vfy.Info{Obligation: hit.Obligation, Fn: hit.Fn, Panic: hit.Kind, Class: "latent", Status: "counterexample", Confirmed: confirmed, Engine: "search", Input: hit.Input, PanicMessage: hit.PanicMessage, Assumes: []string{"no-races"}, Label: "panics for " + call}
			if hit.Runs == 3 {
				in.ReproducedBy = []string{"gc"}
			}
			payload, _ := json.Marshal(in)
			c.Findings = append(c.Findings, diag.Diagnostic{
				Kind: "diagnostic", Severity: diag.Note, Code: diag.CodeVerify,
				Message: fmt.Sprintf("`%s` can panic: %s", shortName(hit.Fn), vfy.KindText(hit.Kind)),
				Span:    diag.Span{File: o.File, Line: o.Line, Col: o.Col, EndLine: o.Line, EndCol: o.Col},
				Primary: in.Label, Notes: []string{note}, Package: hs[0].pkg, Check: "verify", Analyzer: "verify", Verify: payload,
			})
		}
	}
	diag.Sort(c.Findings)
	return notes
}

func shortName(full string) string {
	if i := strings.LastIndex(full, "."); i >= 0 {
		return full[i+1:]
	}
	return full
}

// writeVerifyJSON writes schema 1: the diagnostics, then one result
// line carrying the verify summary.
func writeVerifyJSON(w io.Writer, ds []diag.Diagnostic, sum *vfy.Summary, code int, took time.Duration) {
	errs, warns := diag.Counts(ds)
	_ = diag.WriteJSON(w, ds)
	diag.WriteResult(w, diag.Result{Command: "verify", OK: code == 0, Errors: errs, Warnings: warns, DurationMs: took.Milliseconds(), ExitCode: code, Verify: sum})
}

// VerifyMain runs marmot verify: the verify check alone, its findings as
// diagnostics and its summary in the result line.
func VerifyMain(args []string, version string) int {
	limitMemory()
	fs, vf := checkflags.NewVerify()
	fs.SetOutput(io.Discard)
	err := fs.Parse(args)
	rep := reporter("verify", *vf.JSON, *vf.Color, version)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stdout)
			fs.PrintDefaults()
			return 0
		}
		return rep.Usage(err.Error() + "\nrun `marmot verify -h` for the flags")
	}
	switch *vf.Confirm {
	case "off", "sandbox", "effects":
	default:
		return rep.Usage("marmot verify: -confirm is off, sandbox or effects")
	}
	if *vf.Search && *vf.Confirm == "off" {
		return rep.Usage("marmot verify: -search runs the package's functions with generated inputs; add -confirm=sandbox to allow it")
	}
	opts := Options{Patterns: fs.Args(), Jobs: *vf.Jobs, Only: []string{"verify"}, Latent: *vf.Latent, Offline: true}
	if *vf.Tags != "" {
		opts.Tags = strings.Split(*vf.Tags, ",")
	}
	if !*vf.NoCache {
		opts.CacheDir = checkCacheDir()
	}
	if *vf.Verbose && rep.Mode != diag.ModeJSON {
		opts.Progress = os.Stderr
	}
	start := time.Now()
	res, err := Run(opts)
	if err != nil {
		return rep.Usage("marmot verify: " + err.Error())
	}
	var c *CheckResult
	for _, x := range res.Checks {
		if x.Name == "verify" {
			c = x
		}
	}
	if c == nil {
		return rep.Usage("marmot verify: the verify check did not run")
	}
	if *vf.Confirm != "off" {
		var progress func(string)
		if opts.Progress != nil {
			progress = func(s string) { fmt.Fprintln(opts.Progress, s) }
		}
		notes := verifySearch(c, *vf.Search, *vf.Latent || *vf.Search, *vf.Confirm == "effects", progress)
		if !vfy.SandboxAvailable() {
			notes = append([]string{"no sandbox here (sandbox-exec on macOS, bwrap on Linux): nothing was run, so no finding is confirmed"}, notes...)
		}
		c.Notes = append(c.Notes, notes...)
		if c.Verify == nil {
			c.Verify = &vfy.Summary{}
		}
		c.Verify.Notes = append(c.Verify.Notes, notes...)
	}
	ds := c.Findings
	errs, _ := diag.Counts(ds)
	code := 0
	if c.Status == Failed || errs > 0 {
		code = 1
	}
	sum := c.Verify
	if sum == nil {
		sum = &vfy.Summary{}
	}
	switch rep.Mode {
	case diag.ModeJSON:
		writeVerifyJSON(rep.W, ds, sum, code, time.Since(start))
	default:
		st := diag.Plain
		if rep.Color {
			st = diag.Colors
		}
		w := os.Stdout
		h := &diag.Human{W: w, Style: st, Dir: res.Dir}
		for i := range ds {
			h.RenderOne(&ds[i])
			fmt.Fprintln(w)
		}
		for _, n := range c.Notes {
			fmt.Fprintf(w, "%s%s%s\n", st.Dim, n, st.Reset)
		}
		fmt.Fprintf(w, "verify: %d functions, %d obligations: %d proved, %d manifest, %d latent, %d unknown, %d recovered, %d counterexamples (%.2fs)\n",
			sum.Functions, sum.Obligations, sum.Proved+sum.ProvedAssuming, sum.Manifest, sum.Latent, sum.Unknown, sum.Recovered, sum.Counterexamples, time.Since(start).Seconds())
	}
	return code
}
