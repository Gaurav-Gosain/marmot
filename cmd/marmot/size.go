package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/binsize"
	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

type sizeFlags struct {
	top    *int
	pkg    *string
	why    *string
	output outputFlags
}

func newSizeFlags() (*flag.FlagSet, *sizeFlags) {
	fs := flag.NewFlagSet("size", flag.ContinueOnError)
	f := &sizeFlags{}
	f.top = fs.Int("n", 15, "how many packages and functions to list")
	f.pkg = fs.String("pkg", "", "list the functions of this package only")
	f.why = fs.String("why", "", "show a chain of direct calls from main to this function")
	f.output = addOutputFlags(fs)
	return fs, f
}

// cmdSize implements marmot size [-n N] [-pkg P] [-why F] BINARY.
func cmdSize(args []string) int {
	fs, sf := newSizeFlags()
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError("size", err)
	}
	rep := sf.output.reporter("size")
	if fs.NArg() != 1 {
		return rep.Usage("usage: marmot size [-n N] [-pkg PATH] [-why FUNC] BINARY")
	}
	b, err := binsize.Open(fs.Arg(0))
	if err != nil {
		return rep.Usage("marmot size: " + err.Error())
	}
	if *sf.why != "" {
		chain, ok := b.Why(*sf.why)
		if !ok {
			return rep.Usage(fmt.Sprintf("marmot size: no function %q in %s", *sf.why, fs.Arg(0)))
		}
		if rep.Mode == diag.ModeJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": diag.Schema, "kind": "why", "function": *sf.why, "chain": chain})
			return 0
		}
		if len(chain) == 0 {
			fmt.Printf("%s has no direct callers: it is reached through a function value, an interface, or a table\n", *sf.why)
			return 0
		}
		if chain[len(chain)-1] != *sf.why {
			fmt.Printf("%s is not reached from main by direct calls; its direct callers:\n", *sf.why)
			for _, c := range chain {
				fmt.Println("  " + c)
			}
			return 0
		}
		for i, c := range chain {
			fmt.Printf("%s%s\n", strings.Repeat("  ", i), c)
		}
		return 0
	}
	pkgs := b.PackageSizes()
	funcs := append([]binsize.Func(nil), b.Funcs...)
	if *sf.pkg != "" {
		var keep []binsize.Func
		for _, f := range funcs {
			if f.Pkg == *sf.pkg {
				keep = append(keep, f)
			}
		}
		funcs = keep
	}
	sort.SliceStable(funcs, func(i, j int) bool { return funcs[i].Size > funcs[j].Size })
	if rep.Mode == diag.ModeJSON {
		out := map[string]any{"schema": diag.Schema, "kind": "size", "binary": b, "text": b.TextSize(), "packages": pkgs, "functions": funcs}
		e := json.NewEncoder(os.Stdout)
		e.SetEscapeHTML(false)
		_ = e.Encode(out)
		return 0
	}
	st := diag.Plain
	if rep.Color || diag.IsTerminal(os.Stdout) && os.Getenv("NO_COLOR") == "" && rep.Mode == diag.ModeHuman {
		st = diag.Colors
	}
	fmt.Printf("%s%s%s  %s (%s, %s, names from the %s)\n\n", st.Bold, b.Path, st.Reset, human(uint64(b.FileSize)), b.Format, b.Arch, map[string]string{"pcln": "pc table", "symtab": "symbol table"}[b.Source])
	fmt.Printf("%ssections%s\n", st.Bold, st.Reset)
	secs := append([]binsize.Section(nil), b.Sections...)
	sort.SliceStable(secs, func(i, j int) bool { return secs[i].Size > secs[j].Size })
	for _, s := range secs {
		if s.Size == 0 {
			continue
		}
		fmt.Printf("  %10s  %5.1f%%  %s\n", human(s.Size), pct(s.Size, uint64(b.FileSize)), s.Name)
	}
	text := b.TextSize()
	if *sf.pkg == "" {
		fmt.Printf("\n%scode by package%s (of %s of code)\n", st.Bold, st.Reset, human(text))
		bar := func(n uint64) string {
			w := int(40 * float64(n) / float64(max(text, 1)))
			return st.Gutter + strings.Repeat("#", max(w, 1)) + st.Reset
		}
		for i, p := range pkgs {
			if i == *sf.top {
				var rest uint64
				for _, q := range pkgs[i:] {
					rest += q.Size
				}
				fmt.Printf("  %10s  %5.1f%%  %d more packages\n", human(rest), pct(rest, text), len(pkgs)-i)
				break
			}
			name := p.Name
			if name == "C" {
				name = "(C and assembly: runtime core)"
			}
			fmt.Printf("  %10s  %5.1f%%  %-40s %s\n", human(p.Size), pct(p.Size, text), name, bar(p.Size))
		}
	}
	fmt.Printf("\n%slargest functions%s\n", st.Bold, st.Reset)
	for i, f := range funcs {
		if i == *sf.top {
			break
		}
		fmt.Printf("  %10s  %s\n", human(f.Size), f.Name)
	}
	fmt.Printf("\n%s(marmot size -why FUNC BINARY shows what calls a function)%s\n", st.Dim, st.Reset)
	return 0
}

func human(n uint64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func pct(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}
