package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/Gaurav-Gosain/marmot/internal/pprof"
)

type profFlags struct {
	top          *int
	sample, list *string
	tree         *bool
	minPct       *float64
	output       outputFlags
}

func newProfFlags() (*flag.FlagSet, *profFlags) {
	fs := flag.NewFlagSet("prof", flag.ContinueOnError)
	f := &profFlags{}
	f.top = fs.Int("n", 15, "how many functions to list")
	f.sample = fs.String("sample", "", "sample type to show: cpu, samples, alloc_space, alloc_objects, inuse_space, ... (default: the profile's default)")
	f.list = fs.String("list", "", "show the source of functions matching this regexp, each line with its cost")
	f.tree = fs.Bool("tree", false, "show the call tree (callers above callees) instead of the top list")
	f.minPct = fs.Float64("min", 1, "leave out tree nodes below this percentage of the total")
	f.output = addOutputFlags(fs)
	return fs, f
}

// cmdProf implements marmot prof [flags] PROFILE: a terminal view of a
// pprof CPU or memory profile (top functions, the call tree as a text
// flame graph, source lines with their cost) and JSON for agents.
func cmdProf(args []string) int {
	fs, pf := newProfFlags()
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError("prof", err)
	}
	rep := pf.output.reporter("prof")
	if fs.NArg() != 1 {
		return rep.Usage("usage: marmot prof [-n N] [-tree] [-list REGEXP] [-sample TYPE] PROFILE")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return rep.Usage("marmot prof: " + err.Error())
	}
	p, err := pprof.Parse(data)
	if err != nil {
		return rep.Usage("marmot prof: " + fs.Arg(0) + ": " + err.Error())
	}
	idx, err := p.Index(*pf.sample)
	if err != nil {
		return rep.Usage("marmot prof: " + err.Error())
	}
	unit := p.SampleTypes[idx].Unit
	funcs, total := p.ByFunction(idx)
	if rep.Mode == diag.ModeJSON {
		lines, _ := p.ByLine(idx)
		if len(lines) > 200 {
			lines = lines[:200]
		}
		if len(funcs) > 200 {
			funcs = funcs[:200]
		}
		out := map[string]any{"schema": diag.Schema, "kind": "profile", "sampleType": p.SampleTypes[idx].Type, "unit": unit,
			"total": total, "durationNs": p.DurationNs, "functions": funcs, "lines": lines}
		if *pf.tree {
			out["tree"] = p.Tree(idx)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.Encode(out)
		return 0
	}
	st := diag.Plain
	if diag.IsTerminal(os.Stdout) && os.Getenv("NO_COLOR") == "" {
		st = diag.Colors
	}
	fmt.Printf("%s%s%s: %s %s", st.Bold, fs.Arg(0), st.Reset, p.SampleTypes[idx].Type, fmtValue(total, unit))
	if p.DurationNs > 0 {
		fmt.Printf(" over %s", fmtValue(p.DurationNs, "nanoseconds"))
	}
	fmt.Println()
	if total == 0 {
		fmt.Println("the profile has no samples (the program ran too briefly, or did not use the CPU)")
		return 0
	}
	switch {
	case *pf.list != "":
		re, err := regexp.Compile(*pf.list)
		if err != nil {
			return rep.Usage("marmot prof: bad -list: " + err.Error())
		}
		listSource(p, idx, re, total, unit, st)
	case *pf.tree:
		fmt.Println()
		printTree(p.Tree(idx), total, *pf.minPct, unit, st, "", true)
	default:
		fmt.Printf("\n%s%10s %6s %10s %6s  %s%s\n", st.Bold, "flat", "flat%", "cum", "cum%", "function", st.Reset)
		for i, f := range funcs {
			if i == *pf.top {
				break
			}
			fmt.Printf("%10s %5.1f%% %10s %5.1f%%  %s %s\n", fmtValue(f.Flat, unit), pct64(f.Flat, total), fmtValue(f.Cum, unit), pct64(f.Cum, total),
				f.Name, bar(f.Flat, total, st))
		}
		// The hottest path, top down.
		fmt.Printf("\n%shottest path%s\n", st.Bold, st.Reset)
		// The runtime's entry frames are left out and recursion is folded.
		n := p.Tree(idx)
		depth := 0
		for len(n.Children) > 0 && depth < 20 {
			n = n.Children[0]
			if runtimeEntry[n.Name] {
				continue
			}
			rec, v := 0, n.Value
			for len(n.Children) > 0 && n.Children[0].Name == n.Name && n.Children[0].Value*2 >= n.Value {
				n = n.Children[0]
				rec++
			}
			more := ""
			if rec > 0 {
				more = fmt.Sprintf(", recursive: %d more frames", rec)
			}
			fmt.Printf("%s%s %s(%.1f%%%s)%s\n", strings.Repeat("  ", depth), n.Name, st.Dim, pct64(v, total), more, st.Reset)
			depth++
		}
		fmt.Printf("\n%s(-tree for the call tree, -list FUNC for source lines)%s\n", st.Dim, st.Reset)
	}
	return 0
}

// runtimeEntry names the frames below main.main in a marmot program.
var runtimeEntry = map[string]bool{"rt_ctx_init": true, "gstart": true, "marmot_main": true, "runtime.goexit": true, "runtime.main": true}

func bar(v, total int64, st diag.Style) string {
	w := int(30 * float64(v) / float64(max(total, 1)))
	if w == 0 {
		return ""
	}
	return st.Gutter + strings.Repeat("#", w) + st.Reset
}

func pct64(a, b int64) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}

// fmtValue prints a value in its unit.
func fmtValue(v int64, unit string) string {
	switch unit {
	case "nanoseconds":
		f := float64(v)
		switch {
		case f >= 1e9:
			return fmt.Sprintf("%.2fs", f/1e9)
		case f >= 1e6:
			return fmt.Sprintf("%.1fms", f/1e6)
		case f >= 1e3:
			return fmt.Sprintf("%.1fus", f/1e3)
		}
		return fmt.Sprintf("%dns", v)
	case "bytes":
		return human(uint64(v))
	}
	return fmt.Sprint(v)
}

// printTree prints the call tree as an indented text flame graph.
func printTree(n *pprof.Node, total int64, minPct float64, unit string, st diag.Style, prefix string, root bool) {
	if !root {
		fmt.Printf("%s%s %s%5.1f%% %s%s %s\n", prefix, n.Name, st.Dim, pct64(n.Value, total), fmtValue(n.Value, unit), st.Reset, bar(n.Value, total, st))
		prefix += "  "
	}
	shown := 0
	for _, c := range n.Children {
		if pct64(c.Value, total) < minPct {
			continue
		}
		printTree(c, total, minPct, unit, st, prefix, false)
		shown++
	}
	if rest := len(n.Children) - shown; rest > 0 && !root {
		fmt.Printf("%s%s(%d more below %.1f%%)%s\n", prefix, st.Dim, rest, minPct, st.Reset)
	}
}

// listSource prints the source of the matching functions with the cost of
// each line.
func listSource(p *pprof.Profile, idx int, re *regexp.Regexp, total int64, unit string, st diag.Style) {
	lines, _ := p.ByLine(idx)
	byFunc := map[string]map[int64]pprof.Stat{}
	files := map[string]string{}
	for _, l := range lines {
		if !re.MatchString(l.Name) || l.File == "" {
			continue
		}
		if byFunc[l.Name] == nil {
			byFunc[l.Name] = map[int64]pprof.Stat{}
		}
		s := byFunc[l.Name][l.Line]
		s.Flat += l.Flat
		s.Cum += l.Cum
		byFunc[l.Name][l.Line] = s
		files[l.Name] = l.File
	}
	if len(byFunc) == 0 {
		fmt.Printf("no samples in functions matching %q\n", re.String())
		return
	}
	var names []string
	for n := range byFunc {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		ls := byFunc[n]
		var nums []int64
		for l := range ls {
			nums = append(nums, l)
		}
		sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
		src, _ := os.ReadFile(files[n])
		text := strings.Split(string(src), "\n")
		fmt.Printf("\n%s%s%s  %s%s%s\n", st.Bold, n, st.Reset, st.Dim, files[n], st.Reset)
		first, last := nums[0]-2, nums[len(nums)-1]+2
		if first < 1 {
			first = 1
		}
		for l := first; l <= last; l++ {
			s, has := ls[l]
			src := ""
			if int(l-1) < len(text) {
				src = strings.ReplaceAll(text[l-1], "\t", "    ")
			}
			if has && s.Cum > 0 {
				color := st.Reset
				if pct64(s.Cum, total) >= 5 {
					color = st.Error
				}
				fmt.Printf("%s%10s %10s%s %s%5d |%s %s\n", color, fmtValue(s.Flat, unit), fmtValue(s.Cum, unit), st.Reset, st.Gutter, l, st.Reset, src)
			} else {
				fmt.Printf("%10s %10s %s%5d |%s %s%s%s\n", ".", ".", st.Gutter, l, st.Reset, st.Dim, src, st.Reset)
			}
		}
	}
}
