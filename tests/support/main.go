// Command support generates docs/SUPPORT.md from conformance results, so the numbers in the
// support matrix always come from a committed results file.
//
//	go run ./tests/support -results tests/results.json [-linux FILE] [-out docs/SUPPORT.md]
//
// A results file is the JSON written by tests/conform: {"git": SHA, "results": [...]}. Result names
// may carry an @MODE suffix (dev, release, Oz, O0, O2). Tier tables use the dev results.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type result struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Known  bool   `json:"known_failure"`
}

type report struct {
	Git     string   `json:"git"`
	Results []result `json:"results"`
}

var (
	flResults = flag.String("results", "tests/results.json", "conform results (darwin/arm64)")
	flLinux   = flag.String("linux", "", "conform results from linux/amd64 (scripts/linux/at.sh output)")
	flOut     = flag.String("out", "docs/SUPPORT.md", "output file")
	flKnown   = flag.String("known", "tests/known_failures.txt", "known failures file")
)

func load(path string) *report {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var r report
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	return &r
}

// split returns the test name and its mode ("dev" when the name has no suffix).
func split(n string) (string, string) {
	if i := strings.LastIndex(n, "@"); i >= 0 {
		return n[:i], n[i+1:]
	}
	return n, "dev"
}

type feature struct {
	name     string
	patterns []string // regexps on the test name
	note     string
}

var features = []feature{
	{"Integers: wraparound, shifts, division, conversions", []string{`^run/core/int_`, `hard_int_conv`}, ""},
	{"Floats: NaN, infinities, conversions, printing", []string{`^run/core/float_`, `hard_float_edges`}, "Fused multiply-add follows gc on arm64."},
	{"Complex numbers", []string{`^run/core/complex_`}, ""},
	{"Strings, runes, UTF-8 iteration and conversion", []string{`^run/core/strings_`, `hard_string_edge`}, ""},
	{"Slices: append, copy, aliasing, capacity", []string{`^run/core/slice_`}, "Growth capacities may differ from gc."},
	{"Arrays and structs: copy, compare, embedding", []string{`^run/core/(array_|struct_)`, `hard_struct_array`}, ""},
	{"Maps, all key kinds, iteration with deletion", []string{`^run/core/map_`}, "Iteration order is random, as in gc."},
	{"Methods, method values and expressions", []string{`^run/core/method_`}, ""},
	{"Closures and Go 1.22 loop variables", []string{`^run/core/closure_`, `hard_closure`}, ""},
	{"Interfaces, assertions, type switches", []string{`^run/core/iface_`, `hard_method_set`}, ""},
	{"Generics: functions, types, constraints", []string{`^run/core/generic_`}, ""},
	{"defer, panic, recover", []string{`^run/core/(defer_|panic_)`, `hard_panic`}, ""},
	{"Runtime panics: bounds, nil, divide, assertions", []string{`^run/core/(bounds_|panic_nil|panic_divzero)`}, "Message text matches gc; the stack trace format differs."},
	{"Control flow: labels, goto, switch, range over int and func", []string{`^run/core/ctl_`, `loop_break`}, ""},
	{"Constants and iota", []string{`^run/core/const_`}, ""},
	{"Initialization order and init functions", []string{`^run/core/init_`}, ""},
	{"Evaluation order", []string{`hard_eval_order`, `eval_order_var`}, "Variables are read after calls in the same expression, as gc does."},
	{"Goroutines, channels, select", []string{`^run/core/(goroutine_|chan_|select_)`, `^run/sched/`, `hard_select`, `hard_goroutine`}, ""},
	{"Garbage collection (allocation heavy programs)", []string{`^run/gc/`}, ""},
	{"unsafe", []string{`unsafe_`}, ""},
	{"Compiler optimizations preserve semantics", []string{`^run/opt/`}, "Loop, specialization and float tests; run at -O0, -O2, -Oz and -release."},
	{"Real programs (parsers, interpreters, simulations)", []string{`^run/apps/`}, ""},
}

var staticFeatures = [][3]string{
	{"cgo", "missing", "non-goal for v1"},
	{"Race detector (-race)", "missing", "non-goal for v1"},
	{"plugin package", "missing", "non-goal for v1"},
	{"DWARF debug info", "missing", "non-goal for v1"},
	{"Windows", "missing", "non-goal for v1"},
}

// pkgPaths maps tests/run/std directory names to the import paths they cover.
var pkgPaths = map[string]string{
	"atomic": "sync/atomic", "bufio": "bufio", "bytes": "bytes", "big": "math/big", "compress": "compress/*",
	"context": "context", "encoding": "encoding/{base32,base64,hex,binary,csv,xml,gob,asn1,pem,ascii85}", "errors": "errors",
	"exec": "os/exec", "filepath": "path, path/filepath", "flag": "flag", "fmt": "fmt", "heap": "container/{heap,list,ring}",
	"http": "net/http, httptest, cookiejar, crypto/tls", "io": "io", "json": "encoding/json", "log": "log", "maps": "maps",
	"math": "math, math/bits, math/cmplx", "net": "net, net/netip", "os": "os, os/user", "rand": "math/rand",
	"reflect": "reflect", "regexp": "regexp", "runtime": "runtime, runtime/debug", "sha256": "crypto/{md5,sha1,sha256,sha512}, hash/*",
	"signal": "os/signal", "slices": "slices, cmp", "slog": "log/slog", "sort": "sort", "sql": "database/sql", "strconv": "strconv",
	"strings": "strings", "sync": "sync", "tabwriter": "text/tabwriter", "template": "text/template", "time": "time, time/tzdata",
	"url": "net/url", "utf8": "unicode, unicode/utf8, unicode/utf16",
}

type tally struct{ pass, total int }

func main() {
	flag.Parse()
	rep := load(*flResults)
	if rep == nil {
		fmt.Fprintln(os.Stderr, "support: cannot read", *flResults)
		os.Exit(1)
	}
	dev := map[string]bool{} // test name -> pass, dev mode only
	modes := map[string]*tally{}
	for _, r := range rep.Results {
		n, m := split(r.Name)
		t := modes[m]
		if t == nil {
			t = &tally{}
			modes[m] = t
		}
		t.total++
		if r.Status == "pass" {
			t.pass++
		}
		if m == "dev" {
			dev[n] = r.Status == "pass"
		}
	}
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }

	w("# Marmot support matrix\n\n")
	w("Generated by `go run ./tests/support` from `%s` (compiler tree `%s`, %s). Do not edit by hand.\n\n", *flResults, rep.Git, time.Now().UTC().Format("2006-01-02"))
	w("Status words: **works** (every test passes), **partial** (some fail), **missing** (not implemented), **untested**.\n\n")

	w("## Conformance numbers\n\n")
	w("Every test compares stdout, stderr (up to the first panic line) and exit status with the stock Go toolchain.\n\n")
	w("| Tier | Passing | Total | What it covers |\n| --- | --- | --- | --- |\n")
	tiers := []struct{ prefix, what string }{
		{"run/core/", "language core, no imports, `println` only"}, {"run/gc/", "allocation heavy programs"}, {"run/sched/", "goroutines, channels, deadlock"},
		{"run/std/", "standard library by package"}, {"run/opt/", "optimizer and specializer targets"}, {"run/apps/", "200 to 600 line real programs"},
		{"goroot/", "single file programs from the Go project's own test suite"}, {"world/", "real open source modules (vendored, pinned)"},
	}
	all := tally{}
	for _, t := range tiers {
		c := tally{}
		for n, ok := range dev {
			if strings.HasPrefix(n, t.prefix) {
				c.total++
				if ok {
					c.pass++
				}
			}
		}
		if c.total == 0 {
			continue
		}
		w("| %s | %d | %d | %s |\n", strings.TrimSuffix(t.prefix, "/"), c.pass, c.total, t.what)
		all.pass += c.pass
		all.total += c.total
	}
	w("| **total (dev build)** | **%d** | **%d** | |\n\n", all.pass, all.total)
	var ms []string
	for m := range modes {
		ms = append(ms, m)
	}
	sort.Strings(ms)
	if len(ms) > 1 {
		w("Other build modes in the same run:\n\n| Mode | Passing | Total |\n| --- | --- | --- |\n")
		for _, m := range ms {
			w("| %s | %d | %d |\n", m, modes[m].pass, modes[m].total)
		}
		w("\n")
	}

	w("## Language\n\n| Feature | Status | Tests |\n| --- | --- | --- |\n")
	for _, f := range features {
		c := tally{}
		for n, ok := range dev {
			for _, p := range f.patterns {
				if regexp.MustCompile(p).MatchString(n) {
					c.total++
					if ok {
						c.pass++
					}
					break
				}
			}
		}
		st := "untested"
		switch {
		case c.total > 0 && c.pass == c.total:
			st = "works"
		case c.total > 0 && c.pass > 0:
			st = "partial"
		case c.total > 0:
			st = "missing"
		}
		note := ""
		if f.note != "" {
			note = " " + f.note
		}
		w("| %s | %s | %d of %d%s |\n", f.name, st, c.pass, c.total, note)
	}
	for _, s := range staticFeatures {
		w("| %s | %s | %s |\n", s[0], s[1], s[2])
	}
	w("\n")

	w("## Standard library\n\nEach row is a directory under `tests/run/std`. Packages not listed have no test yet.\n\n")
	w("| Packages | Status | Tests |\n| --- | --- | --- |\n")
	bydir := map[string]*tally{}
	for n, ok := range dev {
		if !strings.HasPrefix(n, "run/std/") {
			continue
		}
		parts := strings.Split(n, "/")
		if len(parts) < 4 {
			continue
		}
		t := bydir[parts[2]]
		if t == nil {
			t = &tally{}
			bydir[parts[2]] = t
		}
		t.total++
		if ok {
			t.pass++
		}
	}
	var dirs []string
	for d := range bydir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		t := bydir[d]
		st := "partial"
		if t.pass == t.total {
			st = "works"
		} else if t.pass == 0 {
			st = "missing"
		}
		p := pkgPaths[d]
		if p == "" {
			p = d
		}
		w("| %s | %s | %d of %d |\n", p, st, t.pass, t.total)
	}
	w("\nThe stock standard library test suites are run with `marmot test`; their pass rates are in `docs/status/rtgo.md` when available.\n\n")

	w("## Real world modules\n\nVendored at a pinned version under `tests/world` (see `tests/world/README.md`), each with a small deterministic program checked against stock Go.\n\n")
	w("| Module test | Status |\n| --- | --- |\n")
	var wn []string
	for n := range dev {
		if strings.HasPrefix(n, "world/") {
			wn = append(wn, n)
		}
	}
	sort.Strings(wn)
	wp := 0
	for _, n := range wn {
		st := "works"
		if dev[n] {
			wp++
		} else {
			st = "fails"
		}
		w("| %s | %s |\n", strings.TrimPrefix(n, "world/"), st)
	}
	if len(wn) > 0 {
		w("\n%d of %d world programs pass.\n", wp, len(wn))
	}
	w("\n")

	w("## Targets\n\n| Target | Status | Tested |\n| --- | --- | --- |\n")
	w("| darwin/arm64 | primary | all tiers above, every commit |\n")
	if lr := load(*flLinux); lr != nil {
		c := tally{}
		for _, r := range lr.Results {
			if _, m := split(r.Name); m == "dev" {
				c.total++
				if r.Status == "pass" {
					c.pass++
				}
			}
		}
		w("| linux/amd64 | cross compiled on the laptop, run on a Linux host | %d of %d conformance tests pass (tree `%s`, see `docs/linux.md`) |\n", c.pass, c.total, lr.Git)
	} else {
		w("| linux/amd64 | cross compiled on the laptop, run on a Linux host | see `docs/linux.md` |\n")
	}
	w("| linux/arm64 | not tested | |\n\n")

	w("## Tools\n\n")
	w("- `marmot build [flags] [packages or files]`: `-o`, `-O {0,1,2,3,s,z}` (default 2), `-release` (whole program, `-O3`, loop and library specializations), `-emit {mlir,llvm,obj,exe}`, `-dialect {llvm,go}`, `-gc {precise,none}`, `-target`, `-tags`, `-nocache`, `-v`, `-x`, `-work`.\n")
	w("- `marmot run [flags] file.go [args]`: build and run.\n")
	w("- `marmot test [-v] [-run RE] [-skip RE] [-bench RE] [-count N] [-timeout D] [packages]`: builds the `go list -test` main and runs it in the package directory.\n")
	w("- `marmot version`.\n\n")

	w("## Known differences from gc\n\n")
	w("- Panic output prints the same message and exits with status 2; the traceback below `goroutine N [running]:` is gc style but frames of inlined functions do not appear.\n")
	w("- Floating point uses the same fused multiply-add rule as gc on arm64 (`x*y+z` fuses, an explicit conversion does not).\n")
	w("- Variables are read after calls in the same expression, as gc does (the spec leaves this unspecified).\n")
	w("- `runtime.Caller` and friends work from a pc table; binaries built with `-s` have none.\n")
	w("- **NaN payloads.** Where the Go specification only says the result is a NaN, marmot may return a different NaN (payload and sign bits) than gc: the builtin `min` and `max` (also over slices, so `slices.Min` and `slices.Max`), `math.Max`, `math.Min`, and `math.Floor`, `Ceil`, `Trunc`, `Round` and `RoundToEven` of a signaling NaN (gc quiets it, marmot may return it unchanged). The value is a NaN in every case; only `math.Float64bits`, `math.Float32bits` or `unsafe` shows the difference, and gc itself differs here across architectures. Values that are not NaN are bit identical to gc on the same architecture; a difference there is a bug. Programs: `tests/run/audit/minmax_nan_payload`, `tests/run/audit/math_exp_log_bits`.\n")
	w("- Slice growth capacities and map iteration order are not guaranteed to equal gc's.\n")
	w("- cgo, the race detector, plugins, DWARF and Windows are not supported.\n")
	w("- `reflect`: see the open failures below for the current gaps.\n\n")

	if kb, err := os.ReadFile(*flKnown); err == nil {
		var open []string
		for _, l := range strings.Split(string(kb), "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "#") {
				continue
			}
			name, why, _ := strings.Cut(l, "#")
			open = append(open, fmt.Sprintf("- `%s`: %s", strings.TrimSpace(name), strings.TrimSpace(why)))
		}
		if len(open) > 0 {
			w("## Open failures\n\nListed in `tests/known_failures.txt`; a test that passes is removed from the list.\n\n%s\n", strings.Join(open, "\n"))
		}
	}
	if err := os.MkdirAll(filepath.Dir(*flOut), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*flOut, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d tests, tree %s)\n", *flOut, all.total, rep.Git)
}
