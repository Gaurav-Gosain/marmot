package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
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

// reporter builds the diagnostic reporter for a command.
func (o outputFlags) reporter(command string) *diag.Reporter {
	jsonFlag, color := false, "auto"
	if o.json != nil {
		jsonFlag, color = *o.json, *o.color
	}
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

// cmdExplain implements marmot explain [--json] CODE.
func cmdExplain(args []string) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	of := addOutputFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rep := of.reporter("explain")
	if fs.NArg() == 0 {
		// List every code.
		if rep.Mode == diag.ModeJSON {
			enc := json.NewEncoder(os.Stdout)
			for _, c := range diag.AllCodes() {
				enc.Encode(c)
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
		return rep.Usage(fmt.Sprintf("marmot explain: unknown code %q (marmot explain lists all)", fs.Arg(0)))
	}
	if rep.Mode == diag.ModeJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.Encode(c)
		return 0
	}
	st := diag.Plain
	if diag.IsTerminal(os.Stdout) && os.Getenv("NO_COLOR") == "" && *of.color != "never" || *of.color == "always" {
		st = diag.Colors
	}
	fmt.Printf("%s%s: %s%s\n\n%s\n", st.Bold, c.Code, c.Title, st.Reset, wrap(c.Text, 80))
	if c.Example != "" {
		fmt.Printf("\n%sExample%s\n\n", st.Bold, st.Reset)
		for _, l := range strings.Split(c.Example, "\n") {
			fmt.Printf("    %s\n", l)
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
	for _, w := range strings.Fields(s) {
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
func cmdFix(args []string) int {
	fs := flag.NewFlagSet("fix", flag.ContinueOnError)
	codesFlag := fs.String("code", "", "only apply fixes for these comma separated codes, for example M0004,M0005")
	tags := fs.String("tags", "", "comma separated build tags")
	of := addOutputFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rep := of.reporter("fix")
	codes := map[string]bool{}
	for _, c := range strings.Split(*codesFlag, ",") {
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
	defer os.RemoveAll(tmp)
	var all []diag.Applied
	var last []diag.Diagnostic
	for round := 0; round < 4; round++ {
		cfg := driver.Config{
			Root: root, Dir: rep.Dir, Output: filepath.Join(tmp, "prog"), OptLevel: "0", Emit: "exe",
			GC: "precise", Dialect: "llvm", Patterns: patterns, Stderr: io.Discard,
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
		applied, werr := diag.ApplyFixes(last, codes)
		if werr != nil {
			return rep.Fail(werr)
		}
		if len(applied) == 0 {
			break
		}
		all = append(all, applied...)
	}
	remaining, _ := diag.Counts(last)
	if rep.Mode == diag.ModeJSON {
		enc := json.NewEncoder(os.Stderr)
		enc.SetEscapeHTML(false)
		for _, a := range all {
			enc.Encode(struct {
				Kind string `json:"kind"`
				diag.Applied
			}{"fix", a})
		}
		if remaining > 0 {
			diag.WriteJSON(os.Stderr, last)
		}
		diag.WriteResult(os.Stderr, diag.Result{Command: "fix", OK: remaining == 0, Errors: remaining, DurationMs: time.Since(rep.Start).Milliseconds()})
	} else {
		for _, a := range all {
			fmt.Printf("%s:%d: %s [%s]\n", diag.RelPath(rep.Dir, a.File), a.Line, a.Title, a.Code)
		}
		switch {
		case len(all) == 0 && remaining == 0:
			fmt.Println("nothing to fix")
		case remaining > 0:
			fmt.Printf("%d fixed, %d errors left\n", len(all), remaining)
		default:
			fmt.Printf("%d fixed, the build is clean\n", len(all))
		}
	}
	if remaining > 0 {
		return 1
	}
	return 0
}
