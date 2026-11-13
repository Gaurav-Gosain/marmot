// Package checkflags defines the command line of marmot check and of the
// analyzer fixes of marmot fix. It has no heavy dependencies: cmd/marmot
// uses it for help and completion, and the analyzers themselves live in
// the separate marmot-check binary that marmot runs.
package checkflags

import (
	"flag"
	"os/exec"
	"strings"
)

// Names are the checks, in the order they run and are reported.
var Names = []string{"fmt", "vet", "lint", "modernize", "gopls", "exhaustive", "nil", "vuln", "tidy", "cross", "deadcode"}

// Check are the flags of marmot check.
type Check struct {
	JSON    *bool
	Color   *string
	Only    *string
	Changed *string
	Tags    *string
	Jobs    *int
	NoCache *bool
	Verbose *bool
	Details *bool
	Offline *bool
	Targets *string
	Watch   *bool
	Staged  *bool
}

// NewCheck returns the flag set of marmot check.
func NewCheck() (*flag.FlagSet, *Check) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	c := &Check{
		JSON:    fs.Bool("json", false, "machine readable output: JSON Lines on stderr (also $MARMOT_OUTPUT=json, and the default under coding agents)"),
		Color:   fs.String("color", "auto", "color in human output: auto, always or never (NO_COLOR turns auto off)"),
		Only:    fs.String("only", "", "run only these checks, comma separated: "+strings.Join(Names, ",")),
		Changed: fs.String("changed", "", "check only what changed against a git ref (-changed alone: origin/main, else main)"),
		Tags:    fs.String("tags", "", "comma separated build tags"),
		Jobs:    fs.Int("j", 0, "parallel analysis jobs (default: the number of CPUs)"),
		NoCache: fs.Bool("nocache", false, "analyze everything again, ignoring cached results"),
		Verbose: fs.Bool("v", false, "print timing and cache statistics"),
		Details: fs.Bool("details", false, "show every finding with its source and suggested fix"),
		Offline: fs.Bool("offline", false, "no network: the vulnerability check is skipped"),
		Watch:   fs.Bool("watch", false, "run again after every change to the files and report only new and resolved findings"),
		Staged:  fs.Bool("staged", false, "check exactly what git has staged (for a pre-commit hook), not the working tree"),
		Targets: fs.String("targets", "", "cross build targets, comma separated goos/goarch (default: scripts/platforms.txt, else linux, darwin and windows on amd64 and arm64)"),
	}
	return fs, c
}

// SplitChanged turns "-changed" without a value into -changed=@default,
// and "-changed REF" into -changed=REF when REF names a commit, so the
// flag works with and without a value.
func SplitChanged(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out = append(out, args[i:]...)
			break
		}
		if a != "-changed" && a != "--changed" {
			out = append(out, a)
			continue
		}
		ref := "@default"
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && isGitRef(args[i+1]) {
			ref = args[i+1]
			i++
		}
		out = append(out, "-changed="+ref)
	}
	return out
}

func isGitRef(s string) bool {
	if strings.HasPrefix(s, ".") || strings.Contains(s, "...") {
		return false
	}
	return exec.Command("git", "rev-parse", "--verify", "-q", s+"^{commit}").Run() == nil
}

// Fix are the analyzer fix flags of marmot fix (run by marmot-check).
type Fix struct {
	JSON    *bool
	Color   *string
	Only    *string
	All     *bool
	Diff    *bool
	Test    *bool
	Upgrade *string
	Tags    *string
	Jobs    *int
}

// NewFix returns the flag set of marmot-check fix.
func NewFix() (*flag.FlagSet, *Fix) {
	fs := flag.NewFlagSet("fix", flag.ContinueOnError)
	f := &Fix{
		JSON:    fs.Bool("json", false, "machine readable output"),
		Color:   fs.String("color", "auto", "color in human output: auto, always or never"),
		Only:    fs.String("only", "", "apply the fixes of these checks, comma separated (default fmt,modernize)"),
		All:     fs.Bool("all", false, "also apply fixes that may be incorrect (vet, lint, exhaustive)"),
		Diff:    fs.Bool("diff", false, "print the changes as patches, change nothing"),
		Test:    fs.Bool("test", false, "the tests of every changed package must still pass"),
		Upgrade: fs.String("upgrade", "", "raise the go version of the modules (go1.N) and apply what it unlocks"),
		Tags:    fs.String("tags", "", "comma separated build tags"),
		Jobs:    fs.Int("j", 0, "parallel analysis jobs"),
	}
	return fs, f
}
