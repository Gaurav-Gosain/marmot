package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"regexp"
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

// usageError reports a bad command line found by the flag package of
// marmot CMD: an unknown flag (with the closest one), a flag without its
// value, a value that does not parse. It returns the exit code 2.
func usageError(cmd string, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	rep := (outputFlags{}).reporter(cmd)
	msg, help := flagUsage(cmd, err.Error())
	return rep.Usage(msg, help...)
}

var (
	reFlagUndefined = regexp.MustCompile(`^flag provided but not defined: -+(\S+)$`)
	reFlagNoValue   = regexp.MustCompile(`^flag needs an argument: -+(\S+)$`)
	reFlagBadValue  = regexp.MustCompile(`^invalid (?:boolean )?value "(.*)" for (?:flag )?-+(\S+?):? (.*)$`)
)

// flagUsage turns a flag package error of marmot CMD into the message and
// help lines of its diagnostic.
func flagUsage(cmd, msg string) (string, []string) {
	flags := "run `marmot " + cmd + " -h` for the flags"
	if m := reFlagUndefined.FindStringSubmatch(msg); m != nil {
		var help []string
		if c := diag.Closest(m[1], commandFlags(cmd)); c != "" {
			help = append(help, "the closest flag is `-"+c+"`")
		}
		return "unknown flag `-" + m[1] + "`", append(help, flags)
	}
	if m := reFlagNoValue.FindStringSubmatch(msg); m != nil {
		return "flag `-" + m[1] + "` needs a value", []string{"write it after the flag: `-" + m[1] + " VALUE` or `-" + m[1] + "=VALUE`", flags}
	}
	if m := reFlagBadValue.FindStringSubmatch(msg); m != nil {
		return "invalid value `" + m[1] + "` for flag `-" + m[2] + "`: " + m[3], []string{flags}
	}
	return msg, []string{flags}
}

// commandFlags lists the flag names of marmot CMD (help excluded).
func commandFlags(cmd string) []string {
	var names []string
	for _, c := range newRoot().Commands() {
		if c.Name() != cmd {
			continue
		}
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Name != "help" {
				names = append(names, f.Name)
			}
		})
	}
	return names
}

// argError reports a missing or extra argument of marmot CMD (got holds
// the arguments given; want names the one it takes) with the usage line.
func argError(rep *diag.Reporter, cmd, usage string, got []string, want string) int {
	help := []string{"the usage is `" + usage + "`", "run `marmot " + cmd + " -h` for the flags"}
	if len(got) == 0 {
		return rep.Usage("missing argument `"+want+"`", help...)
	}
	return rep.Usage("unexpected argument `"+got[len(got)-1]+"`", help...)
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
		command("fmt [files or packages]", "Format Go source and fix its imports (gofmt plus goimports)",
			"Format the Go files of the named packages, files or dir/... trees the way goimports does: gofmt layout, missing imports added, unused imports removed. Files are rewritten in place and listed, like go fmt. -l only lists, -d prints a diff, -check exits 1 when a file is not formatted.",
			"marmot fmt\nmarmot fmt ./...\nmarmot fmt -d main.go\nmarmot fmt -check ./...",
			func() *flag.FlagSet { fs, _ := newFmtFlags(); return fs },
			cmdFmt),
		command("check [packages]", "Run every code check in one pass: fmt, vet, lint, modernize, vuln, tidy, cross, deadcode",
			"Check the workspace (every module of go.work, or the current module) the way gofmt, goimports, go vet, golangci-lint (with .golangci.yml when present), go fix, govulncheck, go mod tidy, cross builds and deadcode would, on one package graph in one process. Results are cached per package, so an unchanged tree is answered at once and an edit reanalyzes only what it affects. Prints one line per finding and one line per check; exits 1 when a gating check fails (modernize and deadcode only report).",
			"marmot check\nmarmot check -changed\nmarmot check -changed main -only vet,lint\nmarmot check -json ./internal/...",
			func() *flag.FlagSet { fs, _ := newCheckFlags(); return fs },
			cmdCheck),
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
		command("mcp", "Serve marmot's tools to coding agents (Model Context Protocol)",
			"Speak the Model Context Protocol on stdin and stdout, exposing build, run, test, fix and explain as tools that return marmot's JSON diagnostics. Register it with an agent, for example: claude mcp add marmot -- marmot mcp",
			"marmot mcp",
			func() *flag.FlagSet { fs, _ := newMCPFlags(); return fs },
			cmdMCP),
		command("size BINARY", "Show what an executable's size is made of",
			"Break an executable down by section, by package and by function, and show what calls a function (-why). Function names and sizes come from marmot's pc table, or from the symbol table for other binaries.",
			"marmot size ./app\nmarmot size -pkg fmt ./app\nmarmot size -why fmt.Sprintf ./app",
			func() *flag.FlagSet { fs, _ := newSizeFlags(); return fs },
			cmdSize),
		command("bench [package]", "Run benchmarks and compare them with go or a saved run",
			"Build a package's benchmarks with marmot, run them several times, and report the median and spread of each. -vs go also builds them with go test and runs both alternately, then reports the change and its significance (Mann-Whitney U, benchstat style). -save keeps the raw results; -compare FILE compares with them.",
			"marmot bench -vs go ./...\nmarmot bench -bench Fib -count 10 .\nmarmot bench -save old.txt . && marmot bench -compare old.txt .",
			func() *flag.FlagSet { fs, _ := newBenchFlags(); return fs },
			cmdBench),
		command("bind HEADER", "Generate Go bindings from a C header",
			"Read a C header through clang and write a cgo file with idiomatic Go bindings: functions with Go types (strings, byte slices, errors from status results, out parameters as results), opaque structs as handle types with methods and Close, enums as typed constants with String, #define constants as consts, and the header's comments as doc comments. What bind cannot decide (who frees a returned pointer, unsupported types) is listed, to be settled in bind.toml. The output is plain cgo and builds with go too.",
			"marmot bind -o zlib.go zlib.h\nmarmot bind -config bind.toml -o sqlite3.go sqlite3.h\nmarmot bind -pkg-config libcurl -o curl.go curl/curl.h",
			func() *flag.FlagSet { fs, _ := newBindFlags(); return fs },
			cmdBind),
		command("sysroot add|list|remove [-libc musl] [GOOS/GOARCH]", "Fetch the C headers and libraries of a target for cross compiling",
			"marmot sysroot add linux/arm64 fetches the glibc and kernel headers, libc's link files and gcc's runtime files of Debian stable for the target (packages checked against Debian's index) into marmot's cache; marmot build -target then compiles cgo packages for it and links the executable here with ld.lld. -libc musl builds musl from its release source for marmot build -static. list shows what is installed, remove deletes one.",
			"marmot sysroot add linux/arm64\nmarmot build -target linux/arm64 -o app .\nmarmot sysroot add -libc musl linux/amd64\nmarmot build -static -target linux/amd64 -o app .\nmarmot sysroot list",
			nil, cmdSysroot),
		command("play [dir]", "Serve a playground: an editor, Run, and the compiler's stages",
			"Serve a page on this machine with an editor for a Go program (the sample, or main.go of dir), Run with its output and exit status, and the program at each stage marmot compiles it through: x/tools SSA, MLIR with the go dialect, LLVM IR and machine code, for dev, -O 2 or -release builds, each line tied to the source line it comes from, plus the binary's size by package. Compiler messages show in the editor. Programs run unconfined with a timeout, so the server listens on the loopback only.",
			"marmot play\nmarmot play -open ./cmd/demo",
			func() *flag.FlagSet { fs, _ := newPlayFlags(); return fs },
			cmdPlay),
		command("cgo show FILE.go", "Show what cgo makes of a file: its C functions, types and exports",
			"Run cgo on the file's package and list, for the file, each C function it calls with its C prototype and how the call is made (fast: the C function calls nothing, so the call is direct; bracketed: the goroutine releases its P while C runs, as it may block or call back into Go), the C types and variables it uses with their Go definitions, its //export functions as C sees them, and the package's #cgo flags. -go prints the file's Go code as marmot compiles it, -c its C wrappers.",
			"marmot cgo show main.go\nmarmot cgo show -go db.go\nmarmot cgo show -json db.go",
			func() *flag.FlagSet { fs, _ := newCgoFlags(); return fs },
			cmdCgo),
		command("doctor", "Check the toolchain, runtime, cache and environment",
			"Check what marmot needs (its code generator and runtime, the go command, LLVM, the system linker), the build cache and free disk, how output is chosen here, and on macOS whether new binaries start slowly; print what to fix.",
			"marmot doctor\nmarmot doctor -json",
			func() *flag.FlagSet { fs, _ := newDoctorFlags(); return fs },
			cmdDoctor),
		command("generate [packages]", "Run //go:generate directives, with their outputs cached",
			"Run the //go:generate directives of the named packages like go generate, and cache what each one writes, keyed by the directive, its environment, the generator binary and the package's other files. A directive whose inputs did not change has its outputs restored instead of running again. -n lists directives and whether they are cached; -force runs everything.",
			"marmot generate ./...\nmarmot generate -n ./...\nmarmot generate -run stringer .",
			func() *flag.FlagSet { fs, _ := newGenerateFlags(); return fs },
			cmdGenerate),
		command("prof PROFILE", "Read a CPU or memory profile in the terminal",
			"Show a pprof profile (from marmot bench -cpuprofile, go test -cpuprofile, or runtime/pprof): the top functions by cost, the hottest path, the call tree as a text flame graph (-tree), or the source of functions with the cost of each line (-list). -json gives agents the same data.",
			"marmot prof cpu.out\nmarmot prof -tree cpu.out\nmarmot prof -list 'parse.*' cpu.out\nmarmot prof -sample alloc_space mem.out",
			func() *flag.FlagSet { fs, _ := newProfFlags(); return fs },
			cmdProf),
		command("cache [trim|clean]", "Show, trim or remove the build cache",
			"Show the size of the build cache. trim shrinks it to MARMOT_CACHE_MAX_MB (default 4096); clean removes it.",
			"marmot cache\nmarmot cache trim", nil, cmdCache),
		versionCommand(),
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
	var err error
	if rep.Mode == diag.ModeHuman && diag.IsTerminal(os.Stdout) {
		// fang styles the help; errors are marmot's own diagnostics.
		fang.GoStyleFlags = true
		err = fang.Execute(context.Background(), root, fang.WithVersion(version), fang.WithErrorHandler(func(io.Writer, fang.Styles, error) {}))
	} else {
		err = root.ExecuteContext(context.Background())
	}
	if err != nil {
		msg, help := cobraUsage(root, args, err.Error())
		return rep.Usage(msg, help...)
	}
	return exitCode
}

var (
	reUnknownCommand = regexp.MustCompile(`^unknown command "([^"]*)" for "([^"]*)"`)
	reUnknownFlag    = regexp.MustCompile(`^unknown flag: (\S+)`)
	reUnknownShort   = regexp.MustCompile(`^unknown shorthand flag: '.' in (\S+)`)
)

// cobraUsage turns an error of cobra's (an unknown command or flag of
// marmot itself; args is the command line) into the message and help
// lines of its diagnostic.
func cobraUsage(root *cobra.Command, args []string, msg string) (string, []string) {
	if m := reUnknownCommand.FindStringSubmatch(msg); m != nil {
		var names []string
		for _, c := range root.Commands() {
			if c.IsAvailableCommand() || c.Name() == "help" {
				names = append(names, c.Name())
				names = append(names, c.Aliases...)
			}
		}
		var help []string
		if c := diag.Closest(m[1], names); c != "" {
			help = append(help, "the closest command is `"+c+"`")
		}
		return "unknown command `" + m[1] + "`", append(help, "run `marmot --help` for the commands")
	}
	m := reUnknownFlag.FindStringSubmatch(msg)
	if m == nil {
		// cobra names what is left after the shorthands it took: "-ersion"
		// for -version. The argument as typed ends with it.
		if m = reUnknownShort.FindStringSubmatch(msg); m != nil {
			for _, a := range args {
				if strings.HasPrefix(a, "-") && strings.HasSuffix(a, m[1][1:]) {
					m[1] = a
					break
				}
			}
		}
	}
	if m != nil {
		var help, names []string
		name := strings.TrimLeft(m[1], "-")
		root.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
		if root.Flags().Lookup(name) != nil {
			help = append(help, "marmot's own flags take two dashes: `--"+name+"`")
		} else if c := diag.Closest(name, names); c != "" {
			help = append(help, "the closest flag is `--"+c+"`")
		}
		for _, f := range commandFlags("build") {
			if f == name {
				// marmot -o app build: a build flag before the command.
				help = append(help, "`-"+name+"` is a flag of `marmot build`; it goes after the command: `marmot build -"+name+" ...`")
				break
			}
		}
		return "unknown flag `" + m[1] + "`", append(help, "run `marmot --help` for the commands and their flags")
	}
	first, _, _ := strings.Cut(msg, "\n")
	return first, []string{"run `marmot --help` for the commands"}
}
