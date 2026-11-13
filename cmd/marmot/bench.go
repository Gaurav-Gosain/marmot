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
	printCompareWidth(os.Stdout, rows, oldName, newName, diag.TermWidth(os.Stdout))
}

// printCompareWidth writes the comparison table for a terminal width w:
// under 100 columns the p value and run counts are left out, a name that
// does not fit takes a line of its own, and notes wrap.
func printCompareWidth(out io.Writer, rows []benchstat.Row, oldName, newName string, w int) {
	units := []string{}
	seen := map[string]bool{}
	nameW := 7 // "geomean"
	for _, r := range rows {
		if !seen[r.Unit] {
			seen[r.Unit] = true
			units = append(units, r.Unit)
		}
		nameW = max(nameW, len(r.Name), len(r.Unit))
	}
	wide := w >= 100
	// Columns after the name: two "value ± spread" columns and the change
	// (and in the wide layout the p value and counts, 16 columns).
	const valW, deltaW = 18, 10
	rest := 2*(valW+1) + deltaW
	if wide {
		rest += 17
	}
	maxName := max(10, w-rest-1)
	if nameW > maxName {
		nameW = maxName
	}
	cell := func(v float64, spread float64, u string) string {
		return fmt.Sprintf("%s ± %.0f%%", benchstat.FormatValue(v, u), 100*spread)
	}
	for _, u := range units {
		fmt.Fprintf(out, "%-*s %*s %*s %*s\n", nameW, u, valW, oldName, valW, newName, deltaW, "vs "+oldName)
		for _, r := range rows {
			if r.Unit != u {
				continue
			}
			delta := "~"
			if r.Significant(0.05) && !math.IsNaN(r.Delta) {
				delta = fmt.Sprintf("%+.1f%%", 100*r.Delta)
			}
			name := r.Name
			if len(name) > nameW {
				fmt.Fprintln(out, name) // a long name on its own line
				name = ""
			}
			line := fmt.Sprintf("%-*s %*s %*s %*s", nameW, name, valW, cell(r.Old.Median, r.Old.Spread, u),
				valW, cell(r.New.Median, r.New.Spread, u), deltaW, delta)
			if wide {
				line += fmt.Sprintf(" (p=%.3f n=%d+%d)", r.P, r.Old.N, r.New.N)
			}
			fmt.Fprintln(out, line)
		}
		if g, n := benchstat.Geomean(rows, u); n > 1 {
			fmt.Fprintf(out, "%-*s %*s %*s %+*.1f%%\n", nameW, "geomean", valW, "", valW, "", deltaW-1, 100*(g-1))
		}
		fmt.Fprintln(out)
	}
	note := "~: no significant difference (p >= 0.05); more runs (-count) narrow it down"
	if !wide {
		note += ". p values and run counts are shown in a terminal of 100 columns or more"
	}
	fmt.Fprint(out, wrapText(note, w))
	for _, r := range rows {
		if r.Unit == "ns/op" && r.New.Median < 1 {
			fmt.Fprint(out, wrapText(fmt.Sprintf("note: %s takes under 1ns per iteration with marmot: the optimizer probably removed the work because its result is unused; store the result in a package level variable", r.Name), w))
		}
	}
}

// wrapText wraps prose at word boundaries to w columns, continuation lines
// indented by two spaces.
func wrapText(s string, w int) string {
	var b strings.Builder
	n := 0
	for _, word := range strings.Fields(s) {
		switch {
		case n == 0:
		case n+1+len(word) > w:
			b.WriteString("\n  ")
			n = 2
		default:
			b.WriteString(" ")
			n++
		}
		b.WriteString(word)
		n += len(word)
	}
	return b.String() + "\n"
}
