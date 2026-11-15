package check

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
		return true
	}
	return true
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
		fmt.Fprintf(w, "verify: %d functions, %d obligations: %d proved, %d manifest, %d latent, %d unknown, %d recovered (%.2fs)\n",
			sum.Functions, sum.Obligations, sum.Proved+sum.ProvedAssuming, sum.Manifest, sum.Latent, sum.Unknown, sum.Recovered, time.Since(start).Seconds())
	}
	return code
}
