// Command support generates docs/support-matrix.md from conformance results, so the numbers in the
// support matrix always come from a committed results file.
//
//	go run ./tests/support -results tests/results.json -linux FILE [-out docs/support-matrix.md]
//
// A results file is the JSON written by tests/conform: {"git": SHA, "results": [...]}. Result names
// may carry an @MODE suffix (dev, release, Oz, O0, O2). Tier tables use the dev results.
//
// Every input is required: the darwin and linux results, tests/support/stock.md (stock package
// tests under marmot test), tests/support/gates.md (opt-in transforms) and the known failures. A
// missing or unreadable input, a notes file without its table, or a conformance tier with no
// results stops the generator with an error naming the section, instead of a page that silently
// lacks it. -partial allows tiers without results (for a page made from a partial run) and
// -linux none leaves the linux row out on purpose.
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
	flLinux   = flag.String("linux", "", "conform results from a linux/amd64 run (required; \"none\" leaves the row out)")
	flOut     = flag.String("out", "docs/support-matrix.md", "output file")
	flStock   = flag.String("stock", "tests/support/stock.md", "stock package test results: a Measured line and a table (package, result, pass, fail, rate)")
	flGates   = flag.String("gates", "tests/support/gates.md", "opt-in transforms: a table (Transform, Switch, Default)")
	flKnown   = flag.String("known", "tests/known_failures.txt", "known failures file")
	flPartial = flag.Bool("partial", false, "allow conformance tiers without results")
)

// fail stops the generator: a page without one of its sections is worse than no new page.
func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "support: "+format+"\n", a...)
	os.Exit(1)
}

// table returns the rows of the first markdown table in path whose header row starts with
// header, as trimmed cells; it fails when the file or the table is missing or has no rows.
func table(path, header, section string, cols int) [][]string {
	b, err := os.ReadFile(path)
	if err != nil {
		fail("%s: %v (input of the %q section)", path, err, section)
	}
	var rows [][]string
	in := false
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if !in {
			in = strings.HasPrefix(l, header)
			continue
		}
		if strings.HasPrefix(l, "| ---") {
			continue
		}
		if !strings.HasPrefix(l, "|") {
			break
		}
		c := strings.Split(strings.Trim(l, "|"), "|")
		if len(c) != cols {
			fail("%s: a row of the %q table has %d cells, want %d: %s", path, section, len(c), cols, l)
		}
		for i := range c {
			c[i] = strings.TrimSpace(c[i])
		}
		rows = append(rows, c)
	}
	if !in {
		fail("%s: no table starting %q (input of the %q section)", path, header, section)
	}
	if len(rows) == 0 {
		fail("%s: the %q table has no rows", path, section)
	}
	return rows
}

// measured returns the text after "Measured:" in path.
func measured(path string) string {
	b, _ := os.ReadFile(path)
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "Measured:"); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	fail("%s: no Measured: line (when and where the stock tests ran)", path)
	return ""
}

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
	{"cgo: C calls and //export, structs, errno, callbacks from C threads, pointer check", []string{`^run/cgo/`, `^world/(cgo_package|sqlite)$`}, "Cross builds need the target's C headers (MARMOT_SYSROOT); the standard library is built without cgo."},
}

var staticFeatures = [][3]string{
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
	// Plain compiler output whoever runs the harness (marmot switches to
	// JSON under coding agents).
	os.Setenv("MARMOT_OUTPUT", "plain")
	flag.Parse()
	// -results takes a comma separated list; later files override earlier ones per test name,
	// so partial runs (one tier at a time) can be combined.
	rep := &report{}
	seen := map[string]int{}
	var shas []string
	for _, f := range strings.Split(*flResults, ",") {
		r := load(f)
		if r == nil {
			fmt.Fprintln(os.Stderr, "support: cannot read", f)
			os.Exit(1)
		}
		shas = append(shas, r.Git)
		for _, x := range r.Results {
			if n, m := split(x.Name); m == "dev" {
				x.Name = n // "NAME@dev" and "NAME" are the same test
			}
			if i, ok := seen[x.Name]; ok {
				rep.Results[i] = x
			} else {
				seen[x.Name] = len(rep.Results)
				rep.Results = append(rep.Results, x)
			}
		}
	}
	rep.Git = shas[len(shas)-1]
	if len(shas) > 1 {
		rep.Git = strings.Join(uniq(shas), ", ")
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

	if *flLinux == "" {
		fail("-linux is required: the results of a linux/amd64 run (or -linux none)")
	}
	w("# Support matrix\n\n")
	w("Generated by `go run ./tests/support` from `%s` (compiler tree `%s`, %s). Do not edit by hand.\n\n", *flResults, rep.Git, time.Now().UTC().Format("2006-01-02"))
	w("Status words: **works** (every test passes), **partial** (some fail), **missing** (not implemented), **untested**.\n\n")

	w("## Conformance numbers\n\n")
	w("Every test compares stdout, stderr (up to the first panic line) and exit status with the stock Go toolchain.\n\n")
	w("| Tier | Passing | Total | What it covers |\n| --- | --- | --- | --- |\n")
	tiers := []struct{ prefix, what string }{
		{"run/core/", "language core, no imports, `println` only"}, {"run/gc/", "allocation heavy programs"}, {"run/sched/", "goroutines, channels, deadlock"},
		{"run/std/", "standard library by package"}, {"run/opt/", "optimizer and specializer targets"}, {"run/mem/", "use after free and lifetime hunting (every way a slice stays reachable)"}, {"run/layout/", "struct layouts: every field mix, every access path"}, {"run/audit/", "counterexamples from the spec and verify audits"}, {"run/apps/", "200 to 600 line real programs"},
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
			if !*flPartial {
				fail("no results for the tier %s: run it, or pass -partial for a page from a partial run", strings.TrimSuffix(t.prefix, "/"))
			}
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
	w("\n### Stock standard library tests under `marmot test`\n\n")
	stock := table(*flStock, "| package |", "stock package tests", 5)
	w("Results of `marmot test -short` on stock packages (%s). \"alloc counts only\" means every failure is a `testing.AllocsPerRun` count.\n\n", measured(*flStock))
	w("| package | result | pass | fail | rate |\n| --- | --- | --- | --- | --- |\n")
	for _, r := range stock {
		w("| %s |\n", strings.Join(r, " | "))
	}
	w("\n")

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

	w("## Build modes\n\n")
	w("- **dev** (default, `-O2`): the safe pipeline; every transform below that is marked off stays off.\n")
	w("- **`-release`**: whole program optimization, `-O3`, loop and library specializations.\n")
	w("- **`-O0` `-O1` `-O2` `-O3` `-Os` `-Oz`**: LLVM level; the conformance runner can run any program in `-O0`, `-O2`, `-Oz` and `-release` (`-modes`).\n")
	w("- **Opt-in lifetime and memory transforms**, each behind an environment switch. A transform is on by default only after a proof review and a full conformance matrix plus fuzz round with it forced on; until then it is off.\n\n")
	w("| Transform | Switch | Default |\n| --- | --- | --- |\n")
	for _, r := range table(*flGates, "| Transform |", "opt-in transforms", 3) {
		w("| %s |\n", strings.Join(r, " | "))
	}
	w("\nStress modes used by the conformance runner: `gcstress` (`MARMOT_GCSTRESS=1`), `poison` (`MARMOT_POISON=1`), `lifetime` (`MARMOT_LIFETIME=all`).\n\n")

	w("## Targets\n\n| Target | Status | Tested |\n| --- | --- | --- |\n")
	w("| darwin/arm64 | primary | all tiers above |\n")
	if *flLinux == "none" {
		// left out on purpose
	} else if lr := load(*flLinux); lr == nil {
		fail("%s: cannot read the linux results", *flLinux)
	} else {
		c := tally{}
		for _, r := range lr.Results {
			if _, m := split(r.Name); m == "dev" {
				c.total++
				if r.Status == "pass" {
					c.pass++
				}
			}
		}
		w("| linux/amd64 | cross compiled on macOS, run on a Linux test host | %d of %d conformance tests pass (tree `%s`) |\n", c.pass, c.total, lr.Git)
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
	w("- Evaluation order: variables are read after the calls of the same expression; open cases are tracked in `tests/run/audit/evalorder_gaps`.\n")
	w("- Unexported methods of embedded types from different packages with the same name: see `world/audit_unexported` under open failures.\n")
	w("- The race detector, plugins, DWARF and Windows are not supported.\n")
	w("- `reflect`: see the open failures below for the current gaps.\n\n")

	kb, err := os.ReadFile(*flKnown)
	if err != nil {
		fail("%s: %v (input of the open failures section)", *flKnown, err)
	}
	{
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

func uniq(a []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, x := range a {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
