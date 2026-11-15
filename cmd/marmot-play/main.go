// Command marmot-play is the browser playground's compiler: marmot's front end alone, built
// for wasip1/wasm and run in a Web Worker (the docs site's /play). It answers one request
// the way the compile service does (marmot play -json) for the stages the front end can
// make: diagnostics, Go SSA and MLIR. The standard library comes from the export data in
// the bundle deploy/play/bundle makes, so only the program is type-checked.
//
//	marmot-play [-mode dev|O2|release] [-stages ssa,mlir] FILE.go
//
// Environment: MARMOT_ROOT (the bundle's /root), MARMOT_MERGED_GOROOT (its /goroot).
// It is smaller than cmd/marmot (no other commands), which keeps it under the static
// hosting's size limit per file.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/Gaurav-Gosain/marmot/internal/driver"
	"github.com/Gaurav-Gosain/marmot/internal/play"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("marmot-play", flag.ContinueOnError)
	mode := fs.String("mode", "dev", "dev, O2 or release")
	stages := fs.String("stages", "ssa,mlir", "the stages, comma separated (ssa, mlir)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: marmot-play [-mode M] [-stages LIST] FILE.go")
		return 2
	}
	src, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot-play:", err)
		return 1
	}
	root, err := driver.FindRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot-play:", err)
		return 2
	}
	q := &play.Request{Mode: *mode, Source: string(src)}
	if *stages != "" {
		q.Stages = strings.Split(*stages, ",")
	}
	srv := play.New(play.Options{Root: root, Run: build, FrontEnd: true})
	res, err := srv.Compile(context.Background(), q)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marmot-play:", err)
		return 2
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(res); err != nil {
		fmt.Fprintln(os.Stderr, "marmot-play:", err)
		return 1
	}
	if !res.OK {
		return 1
	}
	return 0
}

// build runs the one marmot command the compile service's front end uses,
// "build [-O 2|-release] -dialect go -emit mlir -o FILE .", in-process, and returns its
// JSON Lines output as marmot build prints it with MARMOT_OUTPUT=json.
var buildMu sync.Mutex

func build(_ context.Context, dir string, args, env []string) ([]byte, error) {
	buildMu.Lock()
	defer buildMu.Unlock()
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		_ = os.Setenv(k, v) // this process does nothing else
	}
	root, err := driver.FindRoot()
	if err != nil {
		return nil, err
	}
	cfg := driver.Config{Root: root, Dir: dir, Emit: "exe", Patterns: []string{"."}, AllErrors: true}
	if len(args) == 0 || args[0] != "build" {
		return nil, fmt.Errorf("marmot-play runs only marmot build, not %q", args)
	}
	for i := 1; i < len(args); i++ {
		switch a := args[i]; a {
		case "-O":
			i++
			cfg.OptLevel, cfg.OptSet = args[i], true
		case "-release":
			cfg.Release = true
		case "-dialect":
			i++
			cfg.Dialect = args[i]
		case "-emit":
			i++
			cfg.Emit = args[i]
		case "-o":
			i++
			cfg.Output = args[i]
		case ".":
		default:
			return nil, fmt.Errorf("marmot-play: build flag %s is not supported here", a)
		}
	}
	if !strings.HasPrefix(cfg.Output, "/") && cfg.Output != "" {
		cfg.Output = dir + "/" + cfg.Output
	}
	var out bytes.Buffer
	cfg.Stderr = &out
	rep := &diag.Reporter{Settings: diag.Settings{Mode: diag.ModeJSON}, W: &out, Dir: dir, Command: "build"}
	if _, err := driver.Build(cfg); err != nil {
		rep.Fail(err)
		return out.Bytes(), err
	}
	return out.Bytes(), nil
}
