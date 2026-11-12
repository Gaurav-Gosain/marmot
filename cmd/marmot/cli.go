package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/Gaurav-Gosain/marmot/internal/fang"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// exitCode is the status the command run by cobra asks for.
var exitCode int

// command makes a cobra command whose flags are parsed by the flag set
// that flags returns, Go style (-o, -release, -tags=a,b), exactly as the go
// command spells them. The same set is mirrored into cobra for help and
// shell completion.
func command(use, short, long, example string, flags func() *flag.FlagSet, run func(args []string) int) *cobra.Command {
	c := &cobra.Command{
		Use:                use,
		Short:              short,
		Long:               long,
		Example:            example,
		DisableFlagParsing: true,
		RunE: func(c *cobra.Command, args []string) error {
			for _, a := range args {
				if a == "--" {
					break
				}
				if a == "-h" || a == "-help" || a == "--help" {
					return c.Help()
				}
			}
			exitCode = run(args)
			return nil
		},
	}
	if flags != nil {
		mirrorFlags(flags(), c.Flags())
	}
	return c
}

// mirrorFlags copies the definitions of a flag set into cobra's flags (for
// help and completion only; parsing stays with the flag package).
func mirrorFlags(fs *flag.FlagSet, pf *pflag.FlagSet) {
	fs.VisitAll(func(f *flag.Flag) {
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			pf.Bool(f.Name, f.DefValue == "true", f.Usage)
			return
		}
		pf.String(f.Name, f.DefValue, f.Usage)
	})
}

// usageError reports a bad command line: the flag package's message, then
// where to look. It returns the exit code 2.
func usageError(cmd string, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	of := outputFlags{}
	rep := of.reporter(cmd)
	msg := err.Error()
	if rep.Mode != diag.ModeJSON {
		msg += "\nrun `marmot " + cmd + " -h` for the flags"
	}
	return rep.Usage(msg)
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "marmot",
		Short: "Compile Go programs to small, fast native executables through MLIR",
		Long: `marmot compiles Go programs to native executables through MLIR and LLVM.
It works like the go command: run it inside a Go module.

Errors are shown with the source, the exact span, and suggested fixes on a
terminal, as plain file:line:col lines otherwise, and as JSON Lines with
-json or MARMOT_OUTPUT=json (the default under coding agents).`,
		Example: `# build the package in the current directory
marmot build

# build and run a program with arguments
marmot run ./cmd/app -v input.txt

# smallest release binary
marmot build -release -O z -s -o app ./cmd/app

# apply the suggested fixes of the build errors
marmot fix

# what an error code means
marmot explain M0002`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.AddCommand(
		command("build [packages]", "Compile packages and their dependencies",
			"Compile the named main package (default: the current directory) into an executable.",
			"marmot build -o app ./cmd/app\nmarmot build -release ./cmd/app\nmarmot build -target linux/amd64 -o app-linux .",
			func() *flag.FlagSet { fs, _ := newBuildFlags("build"); return fs },
			func(args []string) int { return cmdBuild(args, false) }),
		command("run package|files... [arguments]", "Compile and run a Go program",
			"Build the program in a temporary directory and run it with the arguments that follow the package or the .go files. Its exit status is marmot's.",
			"marmot run main.go\nmarmot run ./cmd/app -port 8080",
			func() *flag.FlagSet { fs, _ := newBuildFlags("run"); return fs },
			func(args []string) int { return cmdBuild(args, true) }),
		command("test [packages]", "Compile and run package tests",
			"Build the test binary of each package and run it, like go test. Test flags (-run, -v, -count, -bench, ...) go to the test binary. With -json, every test and failure is reported as JSON Lines.",
			"marmot test ./...\nmarmot test -run TestParse -v .",
			func() *flag.FlagSet { fs, _ := newTestFlags(); return fs },
			cmdTest),
		command("fix [packages]", "Apply the suggested fixes of build errors",
			"Build, apply every machine applicable fix (missing imports, unused imports and variables, := with no new variables, ...), and build again until no fix applies. -all adds fixes that may be incorrect, such as typo corrections.",
			"marmot fix\nmarmot fix -code M0005 ./cmd/app\nmarmot fix -all",
			func() *flag.FlagSet { fs, _ := newFixFlags(); return fs },
			cmdFix),
		command("explain [CODE]", "Describe an error code",
			"Show what an error code means, an example of the mistake and the fix. With no code, list all codes.",
			"marmot explain M0014\nmarmot explain -json M0002",
			func() *flag.FlagSet { fs, _ := newExplainFlags(); return fs },
			cmdExplain),
		command("watch [build|run|test] [packages]", "Rebuild, rerun or retest on every change",
			"Build now and after every change to a .go file, go.mod or go.sum of the module, using the build cache so only what changed is compiled again. With run, the program is stopped and started again; with test, the tests run again. The screen is cleared before each round on a terminal. Watch flags (-interval, -clear) come first; the other flags go to the build.",
			"marmot watch\nmarmot watch run ./cmd/server -port 8080\nmarmot watch test -run TestParse ./...",
			func() *flag.FlagSet { fs, _ := newWatchFlags(); return fs },
			cmdWatch),
		command("repl", "Evaluate Go interactively",
			"Read Go declarations, statements and expressions and run them as you type, on the incremental build. Expressions print their value; imports of the standard library are added when you use a package. Tab or the right arrow accepts the completion shown in grey. Commands: :type EXPR, :doc NAME, :time, :imports, :reset, :quit.",
			"marmot repl\necho 'strings.Repeat(\"ab\", 3)' | marmot repl",
			func() *flag.FlagSet { fs, _ := newReplFlags(); return fs },
			cmdRepl),
		command("cache [trim|clean]", "Show, trim or remove the build cache",
			"Show the size of the build cache. trim shrinks it to MARMOT_CACHE_MAX_MB (default 4096); clean removes it.",
			"marmot cache\nmarmot cache trim", nil, cmdCache),
		&cobra.Command{
			Use:   "version",
			Short: "Print the marmot version",
			Args:  cobra.NoArgs,
			Run:   func(*cobra.Command, []string) { fmt.Println(version) },
		},
		manCommand(root0(&root)),
	)
	return root
}

// root0 lets manCommand see the root before it is fully built.
func root0(r **cobra.Command) func() *cobra.Command { return func() *cobra.Command { return *r } }

// run executes the command line and returns the exit status.
func run(args []string) int {
	root := newRoot()
	root.SetArgs(args)
	rep := (outputFlags{}).reporter("marmot")
	styled := rep.Mode == diag.ModeHuman && diag.IsTerminal(os.Stdout)
	var err error
	if styled {
		fang.GoStyleFlags = true
		err = fang.Execute(context.Background(), root, fang.WithVersion(version), fang.WithErrorHandler(fangError))
	} else {
		err = root.ExecuteContext(context.Background())
		if err != nil {
			return usageErrorText(rep, err)
		}
	}
	if err != nil {
		return 2
	}
	return exitCode
}

// fangError renders cobra's errors (unknown command, bad arguments) on a
// terminal: the message and the command that helps.
func fangError(w io.Writer, styles fang.Styles, err error) {
	msg := err.Error()
	fmt.Fprintf(w, "%s %s\n", styles.ErrorHeader.Render(" ERROR "), msg)
	if strings.Contains(msg, "unknown command") {
		fmt.Fprintln(w, "run `marmot --help` for the commands")
	}
}

// usageErrorText reports a cobra error without styling.
func usageErrorText(rep *diag.Reporter, err error) int {
	msg := err.Error()
	if rep.Mode != diag.ModeJSON && strings.Contains(msg, "unknown command") {
		msg += "\nrun `marmot --help` for the commands"
	}
	return rep.Usage("marmot: " + msg)
}
