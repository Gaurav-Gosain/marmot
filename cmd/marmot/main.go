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
	"path/filepath"
	"strings"

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
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "build":
		os.Exit(cmdBuild(os.Args[2:], false))
	case "run":
		os.Exit(cmdBuild(os.Args[2:], true))
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "marmot: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func cmdBuild(args []string, run bool) int {
	name := "build"
	if run {
		name = "run"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	out := fs.String("o", "", "output file")
	opt := fs.String("O", "2", "optimization level: 0, 1, 2, 3, s, z")
	emit := fs.String("emit", "exe", "output kind: mlir, llvm, obj, exe")
	work := fs.Bool("work", false, "print and keep the temporary work directory")
	verbose := fs.Bool("v", false, "print progress and timing")
	tags := fs.String("tags", "", "comma separated build tags")
	printCmd := fs.Bool("x", false, "print the commands run")
	nocache := fs.Bool("nocache", false, "do not use the object cache")
	gc := fs.String("gc", "precise", "garbage collector: precise or none")
	target := fs.String("target", "", "target triple (default: host)")
	dialect := fs.String("dialect", "llvm", "MLIR dialect level: llvm or go")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	switch *opt {
	case "0", "1", "2", "3", "s", "z":
	default:
		fmt.Fprintf(os.Stderr, "marmot: bad -O level %q\n", *opt)
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
		Dialect: *dialect, Patterns: patterns, Stderr: os.Stderr,
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
	if err := cmd.Run(); err != nil {
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
