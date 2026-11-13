package check

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/checkflags"
	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/Gaurav-Gosain/marmot/internal/rootdir"
)

// reporter resolves the output mode as every marmot command does.
func reporter(command string, jsonFlag bool, color, version string) *diag.Reporter {
	wd, _ := os.Getwd()
	return &diag.Reporter{
		Settings: diag.Resolve(jsonFlag, color, os.Getenv, diag.IsTerminal(os.Stderr)),
		W:        os.Stderr,
		Dir:      wd,
		Command:  command,
		Start:    time.Now(),
		Version:  version,
		Args:     os.Args[1:],
	}
}

// Main runs marmot check with its command line arguments and returns the
// exit status.
// memoryLimit is the soft heap limit of a run. The live heap of a large
// workspace stays well under it (the analysis holds a few packages at a
// time; dead code holds per-package summaries); without a limit the
// collector lets the heap grow to twice the live size.
const memoryLimit = 1536 << 20

func limitMemory() {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(memoryLimit)
		// Under the limit, collect less often than the default: a warm run
		// allocates much and keeps little, and the limit still bounds a
		// cold one.
		if os.Getenv("GOGC") == "" {
			debug.SetGCPercent(400)
		}
	}
}

func Main(args []string, version string) int {
	limitMemory()
	fs, cf := checkflags.NewCheck()
	fs.SetOutput(io.Discard)
	err := fs.Parse(checkflags.SplitChanged(args))
	rep := reporter("check", *cf.JSON, *cf.Color, version)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stdout)
			fs.PrintDefaults()
			return 0
		}
		return rep.Usage(err.Error() + "\nrun `marmot check -h` for the flags")
	}
	opts := Options{
		Patterns: fs.Args(),
		Jobs:     *cf.Jobs,
		Offline:  *cf.Offline,
	}
	if *cf.Only != "" {
		opts.Only = strings.Split(*cf.Only, ",")
	}
	if *cf.Changed != "" {
		opts.Changed = true
		if *cf.Changed != "@default" {
			opts.Base = *cf.Changed
		}
	}
	if *cf.Tags != "" {
		opts.Tags = strings.Split(*cf.Tags, ",")
	}
	if *cf.Targets != "" {
		opts.Targets = strings.Split(*cf.Targets, ",")
	}
	if !*cf.NoCache {
		opts.CacheDir = checkCacheDir()
	}
	if *cf.Verbose && rep.Mode != diag.ModeJSON {
		opts.Progress = os.Stderr
	}
	if p := os.Getenv("MARMOT_CHECK_CPUPROFILE"); p != "" {
		if f, err := os.Create(p); err == nil && pprof.StartCPUProfile(f) == nil {
			defer pprof.StopCPUProfile()
		}
	}
	if *cf.Watch {
		return Watch(opts, rep, *cf.Details)
	}
	start := time.Now()
	var stagedFrom, stagedTo string
	if *cf.Staged {
		wd, _ := os.Getwd()
		if d, err := filepath.EvalSymlinks(wd); err == nil {
			wd = d
		}
		snap, root, err := stagedTree(wd, opts.CacheDir)
		if err != nil {
			return rep.Usage("marmot check -staged: " + err.Error())
		}
		opts.Dir = snap
		stagedFrom, stagedTo = strings.TrimSuffix(snap, strings.TrimPrefix(wd, root)), root
	}
	res, err := Run(opts)
	if err == nil && stagedFrom != "" {
		mapPaths(res, stagedFrom, stagedTo)
	}
	if p := os.Getenv("MARMOT_CHECK_MEMPROFILE"); p != "" {
		if f, err := os.Create(p); err == nil {
			err := pprof.Lookup("allocs").WriteTo(f, 0)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "marmot check: MARMOT_CHECK_MEMPROFILE:", err)
			}
		}
	}
	if err != nil {
		return rep.Usage("marmot check: " + err.Error())
	}
	if opts.CacheDir != "" {
		if root, err := rootdir.Find(); err == nil {
			// Trimming is housekeeping: the next run that is over the cap
			// trims again.
			_, _, _ = rootdir.TrimCache(root, false)
		}
	}
	code := 0
	if !res.OK() {
		code = 1
	}
	switch rep.Mode {
	case diag.ModeJSON:
		writeCheckJSON(rep, res, start, code)
	default:
		writeCheckText(os.Stdout, rep, res, *cf.Details, *cf.Verbose, time.Since(start))
	}
	return code
}

// checkCacheDir is build/cache/check, under the cache size cap, or the
// user cache directory when marmot runs outside its repository.
func checkCacheDir() string {
	if root, err := rootdir.Find(); err == nil {
		d := filepath.Join(root, "build", "cache", "check")
		if os.MkdirAll(d, 0o755) == nil {
			return d
		}
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "marmot", "check")
	}
	return ""
}

func writeCheckJSON(rep *diag.Reporter, res *Result, start time.Time, code int) {
	var all []diag.Diagnostic
	for _, c := range res.Checks {
		all = append(all, c.Findings...)
	}
	diag.WriteJSON(rep.W, all)
	enc := json.NewEncoder(rep.W)
	enc.SetEscapeHTML(false)
	for _, c := range res.Checks {
		_ = enc.Encode(struct {
			Kind   string `json:"kind"`
			Schema int    `json:"schema"`
			*CheckResult
		}{"check", diag.Schema, c})
	}
	errs, warns := diag.Counts(all)
	diag.WriteResult(rep.W, diag.Result{Command: "check", OK: code == 0, Errors: errs, Warnings: warns, DurationMs: time.Since(start).Milliseconds(), ExitCode: code})
}

func writeCheckText(w io.Writer, rep *diag.Reporter, res *Result, details, verbose bool, took time.Duration) {
	st := diag.Plain
	if rep.Color {
		st = diag.Colors
	}
	h := &diag.Human{W: w, Style: st, Dir: res.Dir}
	for _, c := range res.Checks {
		if len(c.Findings) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s== %s%s\n", st.Bold, c.Name, st.Reset)
		for i := range c.Findings {
			d := &c.Findings[i]
			if details {
				h.RenderOne(d)
				fmt.Fprintln(w)
				continue
			}
			fmt.Fprintln(w, findingLine(res.Dir, d))
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s%-10s %-8s %8s%s\n", st.Bold, "check", "result", "findings", st.Reset)
	for _, c := range res.Checks {
		color := ""
		switch c.Status {
		case Failed:
			color = st.Error
		case Findings:
			color = st.Warning
		case Skipped:
			color = st.Dim
		}
		count := fmt.Sprint(c.Count)
		if c.Status == Skipped {
			count = "-"
		}
		line := fmt.Sprintf("%-10s %s%-8s%s %8s", c.Name, color, c.Status, st.Reset, count)
		if verbose {
			line += fmt.Sprintf("  %6.2fs", c.Duration.Seconds())
		}
		fmt.Fprintln(w, line)
		for _, n := range c.Notes {
			fmt.Fprintf(w, "%s           %s%s\n", st.Dim, n, st.Reset)
		}
	}
	if verbose {
		s := res.Stats
		fmt.Fprintf(w, "%spackages: %d (%d checked), cache: %d hits, %d misses, %d analyzed; list %.2fs, analysis %.2fs; total %.2fs%s\n",
			st.Dim, s.Packages, s.Roots, s.Hits, s.Misses, s.Analyzed, s.ListTime.Seconds(), s.AnalyzeTime.Seconds(), took.Seconds(), st.Reset)
	}
}

// findingLine is the one line form: file:line:col: message (check analyzer).
func findingLine(dir string, d *diag.Diagnostic) string {
	var b strings.Builder
	if d.Span.Valid() {
		fmt.Fprintf(&b, "%s:%d:", diag.RelPath(dir, d.Span.File), d.Span.Line)
		if d.Span.Col > 0 {
			fmt.Fprintf(&b, "%d:", d.Span.Col)
		}
		b.WriteString(" ")
	}
	b.WriteString(d.Message)
	if d.Analyzer != "" {
		fmt.Fprintf(&b, " (%s)", d.Analyzer)
	}
	return b.String()
}

// FixMain runs the analyzer fixes of marmot fix and returns the exit
// status: 0 when the tree is clean or every fix applied, 1 when a fix had
// to be rolled back.
func FixMain(args []string, version string) int {
	limitMemory()
	fs, ff := checkflags.NewFix()
	fs.SetOutput(io.Discard)
	err := fs.Parse(args)
	rep := reporter("fix", *ff.JSON, *ff.Color, version)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stdout)
			fs.PrintDefaults()
			return 0
		}
		return rep.Usage(err.Error())
	}
	fo := FixOptions{Patterns: fs.Args(), Jobs: *ff.Jobs, CacheDir: checkCacheDir(), All: *ff.All, Diff: *ff.Diff, Test: *ff.Test, Upgrade: *ff.Upgrade}
	if *ff.Only != "" {
		fo.Only = strings.Split(*ff.Only, ",")
	}
	if *ff.Tags != "" {
		fo.Tags = strings.Split(*ff.Tags, ",")
	}
	start := time.Now()
	res, err := Fix(fo)
	if err != nil {
		return rep.Usage("marmot fix: " + err.Error())
	}
	code := 0
	if len(res.RolledBack) > 0 {
		code = 1
	}
	if rep.Mode == diag.ModeJSON {
		enc := json.NewEncoder(rep.W)
		enc.SetEscapeHTML(false)
		for _, a := range res.Applied {
			_ = enc.Encode(struct {
				Kind string `json:"kind"`
				Applied
			}{"fix", a})
		}
		for _, a := range res.RolledBack {
			_ = enc.Encode(struct {
				Kind string `json:"kind"`
				Applied
			}{"rolledBack", a})
		}
		if res.Diff != "" {
			_ = enc.Encode(map[string]string{"kind": "diff", "diff": res.Diff})
		}
		diag.WriteResult(rep.W, diag.Result{Command: "fix", OK: code == 0, Errors: len(res.RolledBack), DurationMs: time.Since(start).Milliseconds(), ExitCode: code})
		return code
	}
	if res.Diff != "" {
		fmt.Print(res.Diff)
		return code
	}
	for _, n := range res.Notes {
		fmt.Println(n)
	}
	wd, _ := os.Getwd()
	if d, err := filepath.EvalSymlinks(wd); err == nil {
		wd = d
	}
	for _, a := range res.Applied {
		fmt.Printf("%s:%d: %s [%s %s]\n", diag.RelPath(wd, a.File), a.Line, a.Title, a.Check, a.Analyzer)
	}
	for _, a := range res.RolledBack {
		fmt.Printf("%s:%d: rolled back, the package no longer type checked or its tests failed: %s [%s %s]\n", diag.RelPath(wd, a.File), a.Line, a.Title, a.Check, a.Analyzer)
	}
	switch {
	case len(res.Applied) == 0 && len(res.RolledBack) == 0:
		fmt.Println("nothing to fix")
	default:
		fmt.Printf("%d fixed in %d rounds, %d rolled back\n", len(res.Applied), res.Rounds, len(res.RolledBack))
	}
	return code
}
