package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/benchstat"
	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

type benchFlags struct {
	bench, benchtime, vs, compare, save, timeout, cpuprofile *string
	count                                                    *int
	output                                                   outputFlags
}

func newBenchFlags() (*flag.FlagSet, *benchFlags) {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	b := &benchFlags{}
	b.bench = fs.String("bench", ".", "regexp of the benchmarks to run")
	b.benchtime = fs.String("benchtime", "", "time or iterations per benchmark run, as -test.benchtime")
	b.count = fs.Int("count", 6, "runs of each benchmark (more runs, smaller p-values)")
	b.vs = fs.String("vs", "", "go: also run the benchmarks built by go test and compare")
	b.compare = fs.String("compare", "", "compare with results saved by -save (or any go test -bench output)")
	b.save = fs.String("save", "", "write the raw results to this file")
	b.timeout = fs.String("timeout", "30m", "time limit of each benchmark binary run")
	b.cpuprofile = fs.String("cpuprofile", "", "write a CPU profile of the last run to this file (with -vs go, the go build's to FILE.go); read it with marmot prof")
	b.output = addOutputFlags(fs)
	return fs, b
}

// cmdBench implements marmot bench [flags] [build flags] [package]: run a
// package's benchmarks built by marmot, several times, and compare them
// with go test's build of the same benchmarks (-vs go) or with saved
// results (-compare), benchstat style.
func cmdBench(args []string) int {
	fs, bf := newBenchFlags()
	fs.SetOutput(io.Discard)
	// Bench flags first, build flags after (passed to marmot test -o).
	var buildArgs []string
	var pkg string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		if strings.HasPrefix(a, "-") && fs.Lookup(name) != nil {
			n := 1
			if !strings.Contains(a, "=") && name != "json" && i+1 < len(args) {
				n = 2
			}
			if err := fs.Parse(args[i : i+n]); err != nil {
				return usageError("bench", err)
			}
			i += n - 1
			continue
		}
		if strings.HasPrefix(a, "-") {
			buildArgs = append(buildArgs, a)
			continue
		}
		pkg = a
	}
	if pkg == "" {
		pkg = "."
	}
	rep := bf.output.reporter("bench")
	// Benchmarks of tier0 code (the dev default) measure the fast code
	// generator, not the program: optimize unless told otherwise.
	optimized := false
	for _, a := range buildArgs {
		n := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		if n == "O" || strings.HasPrefix(n, "O") && len(n) == 2 || n == "release" {
			optimized = true
		}
	}
	if !optimized {
		buildArgs = append(buildArgs, "-O", "2")
	}
	tmp, err := os.MkdirTemp("", "marmot-bench-")
	if err != nil {
		return rep.Usage("marmot bench: " + err.Error())
	}
	defer func() { _ = os.RemoveAll(tmp) }() // best effort: a temp dir
	self, _ := os.Executable()
	mbin := filepath.Join(tmp, "marmot.test")
	build := exec.Command(self, append(append([]string{"test", "-o", mbin}, buildArgs...), pkg)...)
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		return 1
	}
	var gbin string
	if *bf.vs == "go" {
		gbin = filepath.Join(tmp, "go.test")
		gb := exec.Command("go", "test", "-c", "-o", gbin, pkg)
		gb.Stdout, gb.Stderr = os.Stdout, os.Stderr
		if err := gb.Run(); err != nil {
			return 1
		}
	} else if *bf.vs != "" {
		return rep.Usage("marmot bench: -vs takes go")
	}
	tmo, err := time.ParseDuration(*bf.timeout)
	if err != nil {
		return rep.Usage("marmot bench: bad -timeout")
	}
	testArgs := []string{"-test.run=^$", "-test.bench=" + *bf.bench, "-test.benchmem", "-test.count=1"}
	if *bf.benchtime != "" {
		testArgs = append(testArgs, "-test.benchtime="+*bf.benchtime)
	}
	var mout, gout bytes.Buffer
	human := rep.Mode == diag.ModeHuman
	for i := 0; i < *bf.count; i++ {
		// Alternate the two binaries so drift in the machine hits both.
		if human {
			fmt.Fprintf(os.Stderr, "\r\x1b[Krun %d of %d", i+1, *bf.count)
		}
		margs, gargs := testArgs, testArgs
		if *bf.cpuprofile != "" && i == *bf.count-1 {
			abs, _ := filepath.Abs(*bf.cpuprofile)
			margs = append(append([]string{}, testArgs...), "-test.cpuprofile="+abs)
			gargs = append(append([]string{}, testArgs...), "-test.cpuprofile="+abs+".go")
		}
		if err := runBench(mbin, margs, &mout, tmo); err != nil {
			fmt.Fprintln(os.Stderr, "\nmarmot bench:", err)
			return 1
		}
		if gbin != "" {
			if err := runBench(gbin, gargs, &gout, tmo); err != nil {
				fmt.Fprintln(os.Stderr, "\nmarmot bench:", err)
				return 1
			}
		}
	}
	if human {
		fmt.Fprint(os.Stderr, "\r\x1b[K")
		fmt.Printf("marmot build flags: %s\n\n", strings.Join(buildArgs, " "))
	}
	if *bf.save != "" {
		if err := os.WriteFile(*bf.save, mout.Bytes(), 0o644); err != nil {
			return rep.Usage("marmot bench: " + err.Error())
		}
	}
	newRes := benchstat.Parse(&mout)
	var oldRes *benchstat.Results
	oldName, newName := "", "marmot"
	switch {
	case gbin != "":
		oldRes, oldName = benchstat.Parse(&gout), "go"
	case *bf.compare != "":
		data, err := os.ReadFile(*bf.compare)
		if err != nil {
			return rep.Usage("marmot bench: " + err.Error())
		}
		oldRes, oldName = benchstat.Parse(bytes.NewReader(data)), filepath.Base(*bf.compare)
	}
	for _, r := range []*benchstat.Results{newRes, oldRes} {
		if r != nil && r.Err != nil {
			return rep.Usage("marmot bench: reading benchmark output: " + r.Err.Error())
		}
	}
	if len(newRes.Names) == 0 {
		return rep.Usage("marmot bench: no benchmarks matched " + *bf.bench)
	}
	if rep.Mode == diag.ModeJSON {
		enc := json.NewEncoder(os.Stdout)
		if oldRes != nil {
			for _, r := range benchstat.Compare(oldRes, newRes) {
				_ = enc.Encode(map[string]any{"schema": diag.Schema, "kind": "bench", "name": r.Name, "unit": r.Unit,
					oldName: r.Old, newName: r.New, "delta": nanNil(r.Delta), "p": r.P, "significant": r.Significant(0.05)})
			}
		} else {
			for _, n := range newRes.Names {
				for _, u := range newRes.Units {
					if v := newRes.Vals[n][u]; len(v) > 0 {
						_ = enc.Encode(map[string]any{"schema": diag.Schema, "kind": "bench", "name": n, "unit": u, newName: benchstat.Summarize(v)})
					}
				}
			}
		}
		return 0
	}
	if oldRes == nil {
		printSingle(newRes)
		return 0
	}
	printCompare(benchstat.Compare(oldRes, newRes), oldName, newName)
	return 0
}

func nanNil(v float64) any {
	if math.IsNaN(v) {
		return nil
	}
	return v
}

func runBench(bin string, args []string, out *bytes.Buffer, tmo time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), tmo)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = filepath.Dir(bin)
	cmd.Env = append(os.Environ(), "MARMOT_OUTPUT=plain")
	cmd.Stdout, cmd.Stderr = out, os.Stderr
	return cmd.Run()
}

func printSingle(r *benchstat.Results) {
	for _, u := range r.Units {
		fmt.Printf("%-40s %14s\n", "", u)
		for _, n := range r.Names {
			v := r.Vals[n][u]
			if len(v) == 0 {
				continue
			}
			s := benchstat.Summarize(v)
			fmt.Printf("%-40s %14s ± %3.0f%%  (n=%d)\n", n, benchstat.FormatValue(s.Median, u), 100*s.Spread, s.N)
			if u == "ns/op" && s.Median < 1 {
				fmt.Printf("  note: under 1ns per iteration: the optimizer probably removed the work; store the result in a package level variable\n")
			}
		}
		fmt.Println()
	}
}

func printCompare(rows []benchstat.Row, oldName, newName string) {
	units := []string{}
	seen := map[string]bool{}
	for _, r := range rows {
		if !seen[r.Unit] {
			seen[r.Unit] = true
			units = append(units, r.Unit)
		}
	}
	for _, u := range units {
		fmt.Printf("%-32s %18s %18s %10s\n", u, oldName, newName, "vs "+oldName)
		for _, r := range rows {
			if r.Unit != u {
				continue
			}
			delta := "~"
			if r.Significant(0.05) && !math.IsNaN(r.Delta) {
				delta = fmt.Sprintf("%+.1f%%", 100*r.Delta)
			}
			fmt.Printf("%-32s %12s ± %2.0f%% %12s ± %2.0f%% %10s (p=%.3f n=%d+%d)\n", r.Name,
				benchstat.FormatValue(r.Old.Median, u), 100*r.Old.Spread,
				benchstat.FormatValue(r.New.Median, u), 100*r.New.Spread, delta, r.P, r.Old.N, r.New.N)
		}
		if g, n := benchstat.Geomean(rows, u); n > 1 {
			fmt.Printf("%-32s %18s %18s %+9.1f%%\n", "geomean", "", "", 100*(g-1))
		}
		fmt.Println()
	}
	fmt.Println("~: no significant difference (p >= 0.05); more runs (-count) narrow it down")
	for _, r := range rows {
		if r.Unit == "ns/op" && r.New.Median < 1 {
			fmt.Printf("note: %s takes under 1ns per iteration with marmot: the optimizer probably removed the work because its result is unused; store the result in a package level variable\n", r.Name)
		}
	}
}
