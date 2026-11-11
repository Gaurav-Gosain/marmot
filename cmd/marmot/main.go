// Command marmot compiles Go programs to native executables through MLIR.
//
//	marmot build [flags] [packages]
//	marmot run [flags] package|files... [arguments]
//	marmot version
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Gaurav-Gosain/marmot/internal/driver"
)

const version = "marmot devel"

func usage() {
	fmt.Fprintf(os.Stderr, `usage: marmot <command> [flags] [packages]

Commands:
  build    compile packages and dependencies
  run      compile and run a Go program
  version  print the marmot version

Run 'marmot build -h' for build flags.
`)
}

func main() {
	stop := startProfile()
	code := realMain()
	stop()
	os.Exit(code)
}

func realMain() int {
	if len(os.Args) < 2 {
		usage()
		return 2
	}
	switch os.Args[1] {
	case "build":
		return cmdBuild(os.Args[2:], false)
	case "run":
		return cmdBuild(os.Args[2:], true)
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "marmot: unknown command %q\n", os.Args[1])
		usage()
		return 2
	}
	return 0
}

func cmdBuild(args []string, run bool) int {
	name := "build"
	if run {
		name = "run"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	out := fs.String("o", "", "output file")
	opt := fs.String("O", "", "optimization level: 0, 1, 2, 3, s, z (default 2, 3 with -release)")
	release := fs.Bool("release", os.Getenv("MARMOT_RELEASE") == "1", "release build: whole program optimization, -O3, all specializations (default from $MARMOT_RELEASE=1)")
	emit := fs.String("emit", "exe", "output kind: mlir, llvm, obj, exe")
	work := fs.Bool("work", false, "print and keep the temporary work directory")
	verbose := fs.Bool("v", false, "print progress and timing")
	tags := fs.String("tags", "", "comma separated build tags")
	printCmd := fs.Bool("x", false, "print the commands run")
	nocache := fs.Bool("nocache", false, "do not use the object cache")
	gc := fs.String("gc", "precise", "garbage collector: precise or none")
	target := fs.String("target", "", "target triple (default: host)")
	dialect := fs.String("dialect", os.Getenv("MARMOT_DIALECT"), "MLIR dialect level: llvm or go (default: go with -release, llvm otherwise; $MARMOT_DIALECT overrides)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *opt == "" {
		*opt = "2"
		if *release {
			*opt = "3"
		}
	}
	switch *opt {
	case "0", "1", "2", "3", "s", "z":
	default:
		fmt.Fprintf(os.Stderr, "marmot: bad -O level %q\n", *opt)
		return 2
	}
	if *dialect == "" {
		*dialect = "llvm"
		if *release {
			*dialect = "go"
		}
	}
	switch *dialect {
	case "llvm", "go":
	default:
		fmt.Fprintf(os.Stderr, "marmot: bad -dialect %q\n", *dialect)
		return 2
	}
	switch *emit {
	case "mlir", "llvm", "obj", "exe":
	default:
		fmt.Fprintf(os.Stderr, "marmot: bad -emit kind %q\n", *emit)
		return 2
	}
	rest := fs.Args()
	var patterns, progArgs []string
	if run {
		// Go files, or a single package, then program arguments.
		i := 0
		for i < len(rest) && strings.HasSuffix(rest[i], ".go") {
			i++
		}
		if i == 0 && len(rest) > 0 {
			i = 1
		}
		patterns, progArgs = rest[:i], rest[i:]
		if *emit != "exe" {
			fmt.Fprintln(os.Stderr, "marmot run: -emit must be exe")
			return 2
		}
	} else {
		patterns = rest
	}
	if len(patterns) == 0 {
		patterns = []string{"."}
	}
	root, err := driver.FindRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot:", err)
		return 2
	}
	wd, _ := os.Getwd()
	cfg := driver.Config{
		Root: root, Dir: wd, Output: *out, OptLevel: *opt, Emit: *emit, Work: *work,
		Verbose: *verbose, PrintCmd: *printCmd, NoCache: *nocache, GC: *gc, Target: *target,
		Dialect: *dialect, Patterns: patterns, Stderr: os.Stderr, Release: *release,
	}
	if *tags != "" {
		cfg.Tags = strings.Split(*tags, ",")
	}
	var tmpDir string
	if run && *out == "" {
		tmpDir, err = os.MkdirTemp("", "marmot-run-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "marmot:", err)
			return 1
		}
		defer os.RemoveAll(tmpDir)
		cfg.Output = filepath.Join(tmpDir, "prog")
	}
	exe, err := driver.Build(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !run {
		return 0
	}
	cmd := exec.Command(exe, progArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "marmot:", err)
		return 1
	}
	// Forward termination signals so the program never outlives marmot.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for s := range sigs {
			cmd.Process.Signal(s)
		}
	}()
	err = cmd.Wait()
	signal.Stop(sigs)
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if code := ee.ExitCode(); code >= 0 {
				return code
			}
			return 1
		}
		fmt.Fprintln(os.Stderr, "marmot:", err)
		return 1
	}
	return 0
}
