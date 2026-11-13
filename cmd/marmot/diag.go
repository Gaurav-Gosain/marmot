package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/Gaurav-Gosain/marmot/internal/driver"
)

// outputFlags are the diagnostic flags every command takes.
type outputFlags struct {
	json  *bool
	color *string
}

func addOutputFlags(fs *flag.FlagSet) outputFlags {
	return outputFlags{
		json:  fs.Bool("json", false, "machine readable output: JSON Lines on stderr (also $MARMOT_OUTPUT=json, and the default under coding agents)"),
		color: fs.String("color", "auto", "color in human output: auto, always or never (NO_COLOR turns auto off)"),
	}
}

// errOut and errWidth are where reporters write and the width they wrap to
// (0: the terminal's); tests set them.
var (
	errOut   io.Writer = os.Stderr
	errWidth int
)

// reporter builds the diagnostic reporter for a command.
func (o outputFlags) reporter(command string) *diag.Reporter {
	jsonFlag, color := false, "auto"
	if o.json != nil {
		jsonFlag, color = *o.json, *o.color
	}
	wd, _ := os.Getwd()
	return &diag.Reporter{
		Settings: diag.Resolve(jsonFlag, color, os.Getenv, diag.IsTerminal(os.Stderr)),
		W:        errOut,
		Width:    errWidth,
		Dir:      wd,
		Command:  command,
		Start:    time.Now(),
		Version:  version,
		Args:     os.Args[1:],
	}
}

// cmdExplain implements marmot explain [--json] CODE.
func newExplainFlags() (*flag.FlagSet, outputFlags) {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	return fs, addOutputFlags(fs)
}

func cmdExplain(args []string) int {
	fs, of := newExplainFlags()
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError("explain", err)
	}
	rep := of.reporter("explain")
	if fs.NArg() == 0 {
		// List every code.
		if rep.Mode == diag.ModeJSON {
			enc := json.NewEncoder(os.Stdout)
			for _, c := range diag.AllCodes() {
				_ = enc.Encode(c)
			}
			return 0
		}
		for _, c := range diag.AllCodes() {
			fmt.Printf("%s  %s\n", c.Code, c.Title)
		}
		return 0
	}
	c := diag.Lookup(fs.Arg(0))
	if c == nil {
		return rep.Usage("unknown error code `"+fs.Arg(0)+"`", "run `marmot explain` for the list of codes")
	}
	if rep.Mode == diag.ModeJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(c)
		return 0
	}
	st := diag.Plain
	if diag.IsTerminal(os.Stdout) && os.Getenv("NO_COLOR") == "" && *of.color != "never" || *of.color == "always" {
		st = diag.Colors
	}
	fmt.Printf("%s%s: %s%s\n\n%s\n", st.Bold, c.Code, c.Title, st.Reset, wrap(c.Text, 80))
	for _, ex := range [][2]string{{"Wrong", c.Wrong}, {"Right", c.Right}} {
		if ex[1] == "" {
			continue
		}
		fmt.Printf("\n%s%s%s\n\n", st.Bold, ex[0], st.Reset)
		for l := range strings.SplitSeq(ex[1], "\n") {
			fmt.Printf("    %s\n", strings.ReplaceAll(l, "\t", "    "))
		}
	}
	if c.Fix != "" {
		fmt.Printf("\n%sFix%s\n\n%s\n", st.Bold, st.Reset, wrap(c.Fix, 80))
	}
	return 0
}

// wrap breaks text into lines of at most n columns.
func wrap(s string, n int) string {
	var b strings.Builder
	col := 0
	for w := range strings.FieldsSeq(s) {
		if col > 0 && col+1+len(w) > n {
			b.WriteByte('\n')
			col = 0
		} else if col > 0 {
			b.WriteByte(' ')
			col++
		}
		b.WriteString(w)
		col += len(w)
	}
	return b.String()
}

// cmdFix implements marmot fix [--code M0004,...] [--json] [build flags] [packages]:
// build, apply the machine applicable fixes of the errors, and repeat while
// fixes apply (a fix can uncover the next error), at most a few rounds.
type fixFlags struct {
	codes, tags *string
	all         *bool
	output      outputFlags
	// Analyzer fixes, run by marmot-check.
	only, upgrade *string
	diff, test    *bool
}

func newFixFlags() (*flag.FlagSet, *fixFlags) {
	fs := flag.NewFlagSet("fix", flag.ContinueOnError)
	f := &fixFlags{}
	f.codes = fs.String("code", "", "only apply fixes for these comma separated codes, for example M0004,M0005")
	f.all = fs.Bool("all", false, "also apply fixes that may be incorrect (typo corrections); check the result")
	f.tags = fs.String("tags", "", "comma separated build tags")
	f.only = fs.String("only", "", "apply only the fixes of these checks (fmt, modernize, vet, lint, exhaustive), not the build errors")
	f.diff = fs.Bool("diff", false, "print the check fixes as patches, change nothing")
	f.test = fs.Bool("test", false, "keep a check fix only if the tests of its package still pass")
	f.upgrade = fs.String("upgrade", "", "raise the go version of the modules to go1.N and apply the modernizers it unlocks")
	f.output = addOutputFlags(fs)
	return fs, f
}

func cmdFix(args []string) int {
	fs, ff := newFixFlags()
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError("fix", err)
	}
	codesFlag, all, tags, of := ff.codes, ff.all, ff.tags, ff.output
	rep := of.reporter("fix")
	// The fixes of marmot check findings (formatting, modernizers, and
	// with -all vet and lint suggestions) come from marmot-check.
	var toolArgs []string
	for _, f := range []struct {
		name, val string
	}{{"only", *ff.only}, {"upgrade", *ff.upgrade}, {"tags", *tags}, {"color", *of.color}} {
		if f.val != "" {
			toolArgs = append(toolArgs, "-"+f.name+"="+f.val)
		}
	}
	for _, f := range []struct {
		name string
		on   bool
	}{{"all", *all}, {"diff", *ff.diff}, {"test", *ff.test}, {"json", *of.json}} {
		if f.on {
			toolArgs = append(toolArgs, "-"+f.name)
		}
	}
	toolArgs = append(toolArgs, fs.Args()...)
	if *codesFlag == "" && (*ff.only != "" || *ff.diff || *ff.test || *ff.upgrade != "") {
		return runCheckTool("fix", toolArgs)
	}
	codes := map[string]bool{}
	for c := range strings.SplitSeq(*codesFlag, ",") {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			if diag.Lookup(c) == nil {
				return rep.Usage(fmt.Sprintf("marmot fix: unknown code %q", c))
			}
			codes[c] = true
		}
	}
	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"."}
	}
	root, err := driver.FindRoot()
	if err != nil {
		return rep.Usage("marmot: " + err.Error())
	}
	tmp, err := os.MkdirTemp("", "marmot-fix-")
	if err != nil {
		return rep.Usage("marmot: " + err.Error())
	}
	defer func() { _ = os.RemoveAll(tmp) }() // best effort: a temp dir
	var done []diag.Applied
	var last []diag.Diagnostic
	for range 4 {
		cfg := driver.Config{
			Root: root, Dir: rep.Dir, Output: filepath.Join(tmp, "prog"), OptLevel: "0", Emit: "exe",
			GC: "precise", Dialect: "llvm", Patterns: patterns, Stderr: io.Discard, AllErrors: true,
		}
		if *tags != "" {
			cfg.Tags = strings.Split(*tags, ",")
		}
		_, err := driver.Build(cfg)
		if err == nil {
			last = nil
			break
		}
		last = rep.Diagnose(err)
		applied, werr := diag.ApplyFixes(last, codes, *all)
		if werr != nil {
			return rep.Fail(werr)
		}
		if len(applied) == 0 {
			break
		}
		done = append(done, applied...)
	}
	remaining, _ := diag.Counts(last)
	if rep.Mode == diag.ModeJSON {
		enc := json.NewEncoder(os.Stderr)
		enc.SetEscapeHTML(false)
		for _, a := range done {
			_ = enc.Encode(struct {
				Kind string `json:"kind"`
				diag.Applied
			}{"fix", a})
		}
		if remaining > 0 {
			diag.WriteJSON(os.Stderr, last)
		}
		// When the check fixes follow, marmot-check writes the one result
		// line of the command.
		if remaining > 0 || *codesFlag != "" {
			diag.WriteResult(os.Stderr, diag.Result{Command: "fix", OK: remaining == 0, Errors: remaining, DurationMs: time.Since(rep.Start).Milliseconds()})
		}
	} else {
		for _, a := range done {
			fmt.Printf("%s:%d: %s [%s]\n", diag.RelPath(rep.Dir, a.File), a.Line, a.Title, a.Code)
		}
		switch {
		case len(done) == 0 && remaining == 0:
			fmt.Println("nothing to fix")
		case remaining > 0:
			fmt.Printf("%d fixed, %d errors left\n", len(done), remaining)
		default:
			fmt.Printf("%d fixed, the build is clean\n", len(done))
		}
	}
	if remaining > 0 {
		return 1
	}
	if *codesFlag == "" {
		// The build is clean: apply the check fixes too.
		return runCheckTool("fix", toolArgs)
	}
	return 0
}

// runTestJSON runs a test binary in JSON mode: -test.v output is parsed
// into one {"kind":"test"} line per test, failures become diagnostics with
// source context, and a result line ends the run.
func runTestJSON(rep *diag.Reporter, cmd *exec.Cmd, pkg string, start time.Time) int {
	verbose := false
	for _, a := range cmd.Args[1:] {
		if a == "-test.v" || strings.HasPrefix(a, "-test.v=") {
			verbose = true
		}
	}
	if !verbose {
		cmd.Args = append(cmd.Args, "-test.v")
	}
	// The parser reads the plain panic text of the test binary.
	cmd.Env = append(os.Environ(), "MARMOT_OUTPUT=plain")
	pr, pw := io.Pipe()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, pw, pw
	if err := cmd.Start(); err != nil {
		return rep.Fail(err)
	}
	done := make(chan []diag.TestResult)
	go func() { done <- diag.ParseTestOutput(pr, pkg, cmd.Dir, nil) }()
	err := cmd.Wait()
	_ = pw.Close() // io.PipeWriter.Close always returns nil
	results := <-done
	enc := json.NewEncoder(rep.W)
	enc.SetEscapeHTML(false)
	failed := 0
	for _, t := range results {
		_ = enc.Encode(t)
		if t.Status == "fail" {
			failed++
		}
	}
	ds := diag.Finish(diag.TestDiagnostics(results), diag.NewSources(), diag.Options{Context: true, Rendered: true, Dir: rep.Dir})
	diag.WriteJSON(rep.W, ds)
	code := 0
	if err != nil {
		code = 1
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() > 0 {
			code = ee.ExitCode()
		}
	}
	diag.WriteResult(rep.W, diag.Result{Command: "test", OK: code == 0, Errors: len(ds), Output: pkg,
		DurationMs: time.Since(start).Milliseconds(), ExitCode: code})
	return code
}
