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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/Gaurav-Gosain/marmot/internal/driver"
)

const version = "marmot devel"

func usage() {
	fmt.Fprintf(os.Stderr, `usage: marmot <command> [flags] [packages]

Commands:
  build    compile packages and dependencies
  run      compile and run a Go program
  test     compile and run package tests (like go test)
  fix      apply the suggested fixes of build errors (imports, unused names, typos)
  explain  describe an error code, for example marmot explain M0002
  version  print the marmot version
  cache    show the build cache size; 'cache trim' trims it to MARMOT_CACHE_MAX_MB
           (default 4096), 'cache clean' removes it

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
	case "test":
		return cmdTest(os.Args[2:])
	case "version":
		fmt.Println(version)
	case "cache":
		return cmdCache(os.Args[2:])
	case "explain":
		return cmdExplain(os.Args[2:])
	case "fix":
		return cmdFix(os.Args[2:])
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
	trace := fs.String("trace", "", "full: inlined frames in tracebacks of -release builds (dev builds always have them)")
	strip := fs.Bool("s", false, "omit the pc table (tracebacks without file:line)")
	pgoGen := fs.Bool("pgo-gen", false, "build an instrumented binary that writes marmot-<id>.profraw next to itself")
	pgo := fs.String("pgo", "", "profile guided optimization with this indexed profile (llvm-profdata merge -o FILE *.profraw)")
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
	of := addOutputFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rep := of.reporter(name)
	optSet := *opt != "" // an explicit -O level selects LLVM over tier0
	if *opt == "" {
		*opt = "2"
		if *release {
			*opt = "3"
		}
	}
	switch *opt {
	case "0", "1", "2", "3", "s", "z":
	default:
		return rep.Usage(fmt.Sprintf("marmot: bad -O level %q", *opt))
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
		return rep.Usage(fmt.Sprintf("marmot: bad -dialect %q", *dialect))
	}
	switch *emit {
	case "mlir", "llvm", "obj", "exe":
	default:
		return rep.Usage(fmt.Sprintf("marmot: bad -emit kind %q", *emit))
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
			return rep.Usage("marmot run: -emit must be exe")
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
		Root: root, Dir: wd, Output: *out, OptLevel: *opt, OptSet: optSet, Emit: *emit, Work: *work,
		Verbose: *verbose, PrintCmd: *printCmd, NoCache: *nocache, GC: *gc, Target: *target,
		Dialect: *dialect, Patterns: patterns, Stderr: os.Stderr, Release: *release, PGOGen: *pgoGen, PGO: *pgo, Strip: *strip, Trace: *trace,
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
		return rep.Fail(err)
	}
	if !run {
		rep.OK(exe)
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

// testFlags are passed to the test binary as -test.NAME; boolean ones take
// no value.
var testFlags = map[string]bool{
	"run": false, "skip": false, "bench": false, "benchtime": false, "count": false, "cpu": false,
	"timeout": false, "parallel": false, "list": false, "shuffle": false,
	"v": true, "short": true, "failfast": true, "benchmem": true,
}

// cmdTest implements marmot test [build flags] [test flags] [packages].
func cmdTest(args []string) int {
	var buildArgs, testArgs, pkgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			pkgs = append(pkgs, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		val := ""
		hasVal := false
		if j := strings.IndexByte(name, '='); j >= 0 {
			name, val, hasVal = name[:j], name[j+1:], true
		}
		name = strings.TrimPrefix(name, "test.")
		if isBool, ok := testFlags[name]; ok {
			if !isBool && !hasVal && i+1 < len(args) {
				i++
				val, hasVal = args[i], true
			}
			if hasVal {
				testArgs = append(testArgs, "-test."+name+"="+val)
			} else {
				testArgs = append(testArgs, "-test."+name)
			}
			continue
		}
		buildArgs = append(buildArgs, a)
		// Build flags with a value.
		switch name {
		case "o", "O", "tags", "dialect", "gc", "target", "emit", "pgo", "color":
			if !hasVal && i+1 < len(args) {
				i++
				buildArgs = append(buildArgs, args[i])
			}
		}
	}
	if len(pkgs) == 0 {
		pkgs = []string{"."}
	}
	status := 0
	for _, p := range pkgs {
		if c := testOne(buildArgs, p, testArgs); c != 0 {
			status = c
		}
	}
	return status
}

func testOne(buildArgs []string, pkg string, testArgs []string) int {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opt := fs.String("O", "", "optimization level")
	tags := fs.String("tags", "", "build tags")
	printCmd := fs.Bool("x", false, "print commands")
	work := fs.Bool("work", false, "keep the work directory")
	release := fs.Bool("release", os.Getenv("MARMOT_RELEASE") == "1", "release build")
	dialect := fs.String("dialect", os.Getenv("MARMOT_DIALECT"), "dialect")
	out := fs.String("o", "", "write the test binary here and do not run it")
	of := addOutputFlags(fs)
	if err := fs.Parse(buildArgs); err != nil {
		return 2
	}
	rep := of.reporter("test")
	optSet := *opt != "" // an explicit -O level selects LLVM over tier0
	if *opt == "" {
		*opt = "2"
		if *release {
			*opt = "3"
		}
	}
	if *dialect == "" {
		*dialect = "llvm"
		if *release {
			*dialect = "go"
		}
	}
	root, err := driver.FindRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot:", err)
		return 2
	}
	wd, _ := os.Getwd()
	exe := *out
	var tmp string
	if exe == "" {
		tmp, err = os.MkdirTemp("", "marmot-test-")
		if err != nil {
			fmt.Fprintln(os.Stderr, "marmot:", err)
			return 1
		}
		defer os.RemoveAll(tmp)
		exe = filepath.Join(tmp, "pkg.test")
	}
	cfg := driver.Config{
		Root: root, Dir: wd, Output: exe, OptLevel: *opt, OptSet: optSet, Emit: "exe", Work: *work,
		PrintCmd: *printCmd, Dialect: *dialect, Release: *release, Patterns: []string{pkg}, Stderr: os.Stderr,
	}
	if *tags != "" {
		cfg.Tags = strings.Split(*tags, ",")
	}
	start := time.Now()
	exe, dir, err := driver.BuildTest(cfg)
	if err != nil {
		rep.Fail(err)
		if rep.Mode != diag.ModeJSON {
			fmt.Printf("FAIL\t%s [build failed]\n", pkg)
		}
		return 1
	}
	if *out != "" {
		return 0
	}
	cmd := exec.Command(exe, testArgs...)
	cmd.Dir = dir
	if cmd.Dir == "" {
		cmd.Dir = wd
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	elapsed := time.Since(start).Seconds()
	if err != nil {
		fmt.Printf("FAIL\t%s\t%.3fs\n", pkg, elapsed)
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() > 0 {
			return ee.ExitCode()
		}
		return 1
	}
	fmt.Printf("ok  \t%s\t%.3fs\n", pkg, elapsed)
	return 0
}

func cmdCache(args []string) int {
	root, err := driver.FindRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot:", err)
		return 1
	}
	mb := func(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }
	switch {
	case len(args) == 0:
		size, n := driver.CacheSize(root)
		fmt.Printf("%s/build/cache: %s in %d files (cap %d MB)\n", root, mb(size), n, capMB())
	case args[0] == "trim":
		before, after, err := driver.TrimCache(root, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, "marmot:", err)
			return 1
		}
		fmt.Printf("cache: %s -> %s\n", mb(before), mb(after))
	case args[0] == "clean":
		if err := driver.CleanCache(root); err != nil {
			fmt.Fprintln(os.Stderr, "marmot:", err)
			return 1
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: marmot cache [trim|clean]")
		return 2
	}
	return 0
}

func capMB() int64 {
	if v, err := strconv.ParseInt(os.Getenv("MARMOT_CACHE_MAX_MB"), 10, 64); err == nil && v >= 0 {
		return v
	}
	return driver.DefaultCacheMB
}
