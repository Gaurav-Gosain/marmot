package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Gaurav-Gosain/marmot/internal/checkflags"
	"github.com/Gaurav-Gosain/marmot/internal/driver"
)

// The analyzers of marmot check live in a separate binary, marmot-check,
// next to marmot: they are large and their package initialization would
// slow down every other command.

func newCheckFlags() (*flag.FlagSet, *checkflags.Check) { return checkflags.NewCheck() }

func cmdCheck(args []string) int { return runCheckTool("check", args) }

// checkTool finds marmot-check: $MARMOT_CHECK, next to this executable,
// or in build/bin of the marmot root.
func checkTool() (string, error) {
	if p := os.Getenv("MARMOT_CHECK"); p != "" {
		return p, nil
	}
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		if exe, err := filepath.EvalSymlinks(exe); err == nil {
			dirs = append(dirs, filepath.Dir(exe))
		}
	}
	if root, err := driver.FindRoot(); err == nil {
		dirs = append(dirs, filepath.Join(root, "build", "bin"))
	}
	for _, d := range dirs {
		p := filepath.Join(d, "marmot-check")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("marmot-check is not installed next to marmot; build it with `make check-tool` in the marmot repository")
}

// runCheckTool runs a subcommand of marmot-check with the same standard
// streams and returns its exit status.
func runCheckTool(sub string, args []string) int {
	tool, err := checkTool()
	if err != nil {
		rep := (outputFlags{}).reporter(sub)
		return rep.Usage("marmot " + sub + ": " + err.Error())
	}
	cmd := exec.Command(tool, append([]string{sub}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "MARMOT_VERSION="+version)
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() >= 0 {
			return ee.ExitCode()
		}
		rep := (outputFlags{}).reporter(sub)
		return rep.Usage(fmt.Sprintf("marmot %s: %v", sub, err))
	}
	return 0
}
