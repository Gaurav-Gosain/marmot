// Command report turns a bench results file into REPORT.md, report.html and chart images.
//
//	go run ./bench/report [-in bench/results/FILE.json] [-nopng]
//
// With no -in it uses the newest file in bench/results.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Stat struct {
	Min    float64 `json:"min"`
	Median float64 `json:"median"`
	Mean   float64 `json:"mean"`
	Stddev float64 `json:"stddev"`
	Runs   int     `json:"runs"`
}

type Result struct {
	Program      string `json:"program"`
	Tier         string `json:"tier"`
	Toolchain    string `json:"toolchain"`
	Status       string `json:"status"`
	Error        string `json:"error"`
	BuildCold    *Stat  `json:"build_cold_ms"`
	BuildWarm    *Stat  `json:"build_warm_ms"`
	Size         int64  `json:"size_bytes"`
	SizeStripped int64  `json:"size_stripped_bytes"`
	Run          *Stat  `json:"run_ms"`
	RSS          int64  `json:"peak_rss_bytes"`
	OutputMatch  *bool  `json:"output_match"`
}

type Meta struct {
	Date      string            `json:"date"`
	Label     string            `json:"label"`
	CPU       string            `json:"cpu"`
	NCPU      int               `json:"ncpu"`
	MemBytes  int64             `json:"mem_bytes"`
	OS        string            `json:"os"`
	LoadAvg   string            `json:"load_avg_at_start"`
	Versions  map[string]string `json:"versions"`
	Flags     map[string]string `json:"flags"`
	RunRuns   int               `json:"run_runs"`
	WarmRuns  int               `json:"warm_build_runs"`
	ColdRuns  int               `json:"cold_build_runs"`
	GitRev    string            `json:"git_rev"`
	Hyperfine string            `json:"hyperfine"`
	Notes     string            `json:"notes"`
}

type File struct {
	Meta    Meta     `json:"meta"`
	Results []Result `json:"results"`
}

// Metric extracts one number from a result. ok is false when the value is missing.
type Metric struct {
	Key, Title, Unit string
	Get              func(r Result) (float64, bool)
	// Base picks the baseline toolchain value for a program.
	Base string
	// Series lists the toolchains charted against the baseline.
	Series []string
	// Fmt formats an absolute value for tables.
	Fmt func(float64) string
}

var (
	in    = flag.String("in", "", "results file (default: newest in bench/results)")
	nopng = flag.Bool("nopng", false, "skip PNG conversion")
	root  string
)

func main() {
	flag.Parse()
	root = findRoot()
	path := *in
	if path == "" {
		m, _ := filepath.Glob(filepath.Join(root, "bench", "results", "*.json"))
		sort.Strings(m)
		if len(m) == 0 {
			fatal("no results files")
		}
		path = m[len(m)-1]
	}
	data, err := os.ReadFile(path)
	check(err)
	var f File
	check(json.Unmarshal(data, &f))

	idx := map[string]Result{}
	var progs []string
	seen := map[string]bool{}
	tools := []string{}
	seenT := map[string]bool{}
	tier := map[string]string{}
	for _, r := range f.Results {
		idx[r.Program+"|"+r.Toolchain] = r
		if !seen[r.Program] {
			seen[r.Program] = true
			progs = append(progs, r.Program)
		}
		if !seenT[r.Toolchain] {
			seenT[r.Toolchain] = true
			tools = append(tools, r.Toolchain)
		}
		tier[r.Program] = r.Tier
	}
	sort.SliceStable(progs, func(i, j int) bool {
		if tier[progs[i]] != tier[progs[j]] {
			return tier[progs[i]] < tier[progs[j]]
		}
		return progs[i] < progs[j]
	})
	order := []string{"go", "go-strip", "tinygo-O2", "tinygo-Oz", "marmot-O2", "marmot-Oz"}
	sort.SliceStable(tools, func(i, j int) bool { return rank(order, tools[i]) < rank(order, tools[j]) })

	ms := f64
	metrics := []Metric{
		{Key: "runtime", Title: "Run time against Go (lower is faster)", Base: "go",
			Series: []string{"tinygo-O2", "marmot-O2"},
			Get: func(r Result) (float64, bool) {
				if r.Run == nil || r.Status != "ok" {
					return 0, false
				}
				return r.Run.Median, true
			}, Fmt: fmtMS},
		{Key: "size", Title: "Stripped binary size against Go (lower is smaller)", Base: "go-strip",
			Series: []string{"tinygo-Oz", "marmot-Oz"},
			Get: func(r Result) (float64, bool) {
				if r.Size == 0 {
					return 0, false
				}
				if r.Toolchain == "go-strip" {
					return ms(r.Size), true
				}
				if r.SizeStripped == 0 {
					return ms(r.Size), true
				}
				return ms(r.SizeStripped), true
			}, Fmt: fmtBytes},
		{Key: "build_warm", Title: "Warm build time against Go (lower is faster)", Base: "go",
			Series: []string{"tinygo-O2", "marmot-O2"},
			Get: func(r Result) (float64, bool) {
				if r.BuildWarm == nil {
					return 0, false
				}
				return r.BuildWarm.Median, true
			}, Fmt: fmtMS},
		{Key: "build_cold", Title: "Cold build time against Go (lower is faster)", Base: "go",
			Series: []string{"tinygo-O2", "marmot-O2"},
			Get: func(r Result) (float64, bool) {
				if r.BuildCold == nil {
					return 0, false
				}
				return r.BuildCold.Median, true
			}, Fmt: fmtMS},
		{Key: "rss", Title: "Peak memory against Go (lower is smaller)", Base: "go",
			Series: []string{"tinygo-O2", "marmot-O2"},
			Get: func(r Result) (float64, bool) {
				if r.RSS == 0 {
					return 0, false
				}
				return ms(r.RSS), true
			}, Fmt: fmtBytes},
	}

	get := func(m Metric, prog, tool string) (float64, bool) {
		r, ok := idx[prog+"|"+tool]
		if !ok {
			return 0, false
		}
		return m.Get(r)
	}

	var md strings.Builder
	var htmlSections strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&md, format, a...) }

	w("# marmot benchmark report\n\n")
	w("Generated from `%s`. Do not edit by hand: run `make bench` and `go run ./bench/report`.\n\n", rel(path))
	w("## Setup\n\n")
	w("- Date: %s, label `%s`, git `%s`\n", f.Meta.Date, f.Meta.Label, f.Meta.GitRev)
	w("- Machine: %s, %d cores, %d GB RAM, %s\n", f.Meta.CPU, f.Meta.NCPU, f.Meta.MemBytes>>30, f.Meta.OS)
	w("- Load average at start: %s (a shared machine adds noise)\n", strings.Trim(f.Meta.LoadAvg, "{ }"))
	for _, k := range []string{"go", "tinygo", "marmot"} {
		if v := f.Meta.Versions[k]; v != "" {
			w("- %s: %s\n", k, v)
		}
	}
	w("- Timing: %s. Run time %d runs after 1 warmup, warm build %d runs after 1 warmup, cold build %d runs with the cache cleared before each.\n",
		f.Meta.Hyperfine, f.Meta.RunRuns, f.Meta.WarmRuns, f.Meta.ColdRuns)
	w("- Tables show the median. Ratios are against Go; below 1.00 is better than Go.\n")
	if f.Meta.Notes != "" {
		w("- Notes: %s\n", f.Meta.Notes)
	}
	w("\nToolchain flags:\n\n")
	for _, t := range tools {
		w("- `%s`: `%s`\n", t, f.Meta.Flags[t])
	}
	w("\nOutput of every binary is compared with stock Go (stdout and stderr). A row marked FAIL did not build or did not run, MISMATCH built but printed something different. Neither is hidden.\n\n")

	// Geomean summary.
	w("## Headline: geometric mean ratios against Go\n\n")
	w("| Metric | Baseline |")
	for _, t := range tools {
		w(" %s |", t)
	}
	w("\n|---|---|")
	for range tools {
		w("---|")
	}
	w("\n")
	for _, m := range metrics {
		w("| %s | %s |", m.Key, m.Base)
		for _, t := range tools {
			g, n := geomean(m, progs, t, get)
			if n == 0 {
				w(" n/a |")
			} else {
				w(" %.2f (%d) |", g, n)
			}
		}
		w("\n")
	}
	w("\nThe number in parentheses is how many programs entered the mean. Only programs that built, ran and matched in both the toolchain and its baseline count. The size baseline is `go-strip`; every other size is measured after `strip`.\n\n")

	// Per metric tables and charts.
	for _, m := range metrics {
		title := strings.SplitN(m.Title, " (", 2)[0]
		w("## %s\n\n![%s](assets/%s.png)\n\n", title, m.Title, m.Key)
		w("| Program | Tier |")
		for _, t := range tools {
			w(" %s |", t)
		}
		w("\n|---|---|")
		for range tools {
			w("---|")
		}
		w("\n")
		for _, p := range progs {
			w("| %s | %s |", p, tier[p])
			for _, t := range tools {
				w(" %s |", cell(m, idx, p, t, get))
			}
			w("\n")
		}
		g := []string{}
		for _, t := range tools {
			if v, n := geomean(m, progs, t, get); n > 0 {
				g = append(g, fmt.Sprintf("%s %.2f", t, v))
			}
		}
		w("\nGeometric mean ratio: %s.\n\n", strings.Join(g, ", "))

		svg := chart(f.Meta, m, progs, tier, get)
		base := filepath.Join(root, "bench", "assets", m.Key)
		check(os.WriteFile(base+".svg", []byte(svg), 0o644))
		if !*nopng {
			if err := exec.Command("rsvg-convert", "-o", base+".png", "-b", "white", base+".svg").Run(); err != nil {
				fmt.Fprintln(os.Stderr, "png:", err)
			}
		}
		htmlSections.WriteString("<section><h2>" + html.EscapeString(title) + "</h2>" + svg + "</section>\n")
	}

	// Hello world.
	w("## Hello world\n\nSize is the first thing people check. `hello` uses only `println`, `hellostd` uses `fmt`.\n\n")
	w("| Program | Toolchain | Size | Stripped | Startup (median) | Status |\n|---|---|---|---|---|---|\n")
	for _, p := range []string{"hello", "hellostd"} {
		for _, t := range tools {
			r, ok := idx[p+"|"+t]
			if !ok {
				continue
			}
			st := "-"
			if r.Run != nil {
				st = fmtMS(r.Run.Median)
			}
			w("| %s | %s | %s | %s | %s | %s |\n", p, t, fmtBytes(ms(r.Size)), fmtBytes(ms(r.SizeStripped)), st, strings.ToUpper(r.Status))
		}
	}
	w("\n")

	// Failures.
	w("## Failures and mismatches\n\n")
	n := 0
	for _, r := range f.Results {
		if r.Status != "ok" {
			n++
			msg := strings.SplitN(strings.TrimSpace(r.Error), "\n", 2)[0]
			if len(msg) > 120 {
				msg = msg[:120] + "..."
			}
			w("- `%s` on `%s`: %s. %s\n", r.Program, r.Toolchain, strings.ToUpper(r.Status), strings.ReplaceAll(msg, "|", "/"))
		}
	}
	if n == 0 {
		w("None.\n")
	}
	w("\n## Method notes\n\n")
	w("- Programs live in `bench/programs/NAME/main.go`. The `nostd` tier imports nothing and reports through `println`, which writes to stderr. The `std` tier uses the standard library.\n")
	w("- Expected output for each program is stored in `bench/expected/` and was produced by stock Go. Regenerate with `go run ./bench/cmd/bench -update-expected`.\n")
	w("- Cold build clears the Go build cache and runs `tinygo clean` before each sample, so the standard library is rebuilt. For marmot, cold means `-nocache`.\n")
	w("- Warm build repeats the same command with caches populated. Go still links each time.\n")
	w("- Peak RSS is the median of three `/usr/bin/time -l` runs.\n")
	w("- TinyGo uses its default scheduler and GC for the host target. Programs that need more of the language than TinyGo supports are listed as failures.\n")
	check(os.WriteFile(filepath.Join(root, "bench", "REPORT.md"), []byte(md.String()), 0o644))

	page := "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>marmot benchmarks</title><style>" +
		"body{font:15px/1.5 -apple-system,Helvetica,Arial,sans-serif;max-width:1000px;margin:0 auto;padding:16px;color:#1b1f24;background:#fff}" +
		"h1,h2{line-height:1.2}section{margin:32px 0}svg{max-width:100%;height:auto}pre{white-space:pre-wrap;background:#f4f5f7;padding:12px;border-radius:6px;font-size:13px}" +
		"@media(prefers-color-scheme:dark){body{background:#14161a;color:#e6e8eb}pre{background:#1d2026}}</style></head><body>" +
		"<h1>marmot benchmarks</h1><p>" + html.EscapeString(fmt.Sprintf("%s. %s. Run %s, label %s.", f.Meta.CPU, f.Meta.Versions["go"], f.Meta.Date, f.Meta.Label)) +
		" Charts are on white backgrounds. Tables and notes are in REPORT.md.</p>" + htmlSections.String() +
		"<section><h2>Details</h2><pre>" + html.EscapeString(md.String()) + "</pre></section></body></html>\n"
	check(os.WriteFile(filepath.Join(root, "bench", "report.html"), []byte(page), 0o644))
	fmt.Println("wrote bench/REPORT.md, bench/report.html, bench/assets/")
}

func f64(v int64) float64 { return float64(v) }

func rank(order []string, s string) int {
	for i, o := range order {
		if o == s {
			return i
		}
	}
	return len(order)
}

func geomean(m Metric, progs []string, tool string, get func(Metric, string, string) (float64, bool)) (float64, int) {
	sum, n := 0.0, 0
	for _, p := range progs {
		b, ok1 := get(m, p, m.Base)
		v, ok2 := get(m, p, tool)
		if ok1 && ok2 && b > 0 && v > 0 {
			sum += math.Log(v / b)
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return math.Exp(sum / float64(n)), n
}

func cell(m Metric, idx map[string]Result, p, t string, get func(Metric, string, string) (float64, bool)) string {
	r, ok := idx[p+"|"+t]
	if !ok {
		return ""
	}
	switch r.Status {
	case "build_failed", "run_failed":
		return "FAIL"
	case "mismatch":
		if m.Key == "size" || strings.HasPrefix(m.Key, "build") {
			break
		}
		return "MISMATCH"
	}
	v, ok := get(m, p, t)
	if !ok {
		return ""
	}
	s := m.Fmt(v)
	if b, ok := get(m, p, m.Base); ok && t != m.Base && b > 0 {
		s += fmt.Sprintf(" (%.2fx)", v/b)
	}
	return s
}

func fmtMS(v float64) string {
	switch {
	case v >= 10000:
		return fmt.Sprintf("%.1f s", v/1000)
	case v >= 1000:
		return fmt.Sprintf("%.2f s", v/1000)
	case v >= 10:
		return fmt.Sprintf("%.0f ms", v)
	}
	return fmt.Sprintf("%.1f ms", v)
}

func fmtBytes(v float64) string {
	switch {
	case v <= 0:
		return "-"
	case v >= 1<<20:
		return fmt.Sprintf("%.2f MB", v/(1<<20))
	case v >= 1<<10:
		return fmt.Sprintf("%.0f KB", v/(1<<10))
	}
	return fmt.Sprintf("%.0f B", v)
}

var palette = map[string]string{
	"tinygo-O2": "#e07b00", "tinygo-Oz": "#e07b00",
	"marmot-O2": "#1a73e8", "marmot-Oz": "#1a73e8",
	"go-strip": "#6b7280", "go": "#6b7280",
}

// chart draws a horizontal bar chart of per-program ratios against the baseline on a log axis.
func chart(meta Meta, m Metric, progs []string, tier map[string]string, get func(Metric, string, string) (float64, bool)) string {
	type row struct {
		name string
		vals []float64 // per series, 0 when missing
		fail []string  // reason when the series has no value
	}
	var series []string
	for _, s := range m.Series {
		series = append(series, s)
	}
	var rows []row
	lo, hi := 1.0, 1.0
	for _, p := range progs {
		r := row{name: p, vals: make([]float64, len(series)), fail: make([]string, len(series))}
		b, okb := get(m, p, m.Base)
		for i, s := range series {
			v, ok := get(m, p, s)
			if ok && okb && b > 0 {
				r.vals[i] = v / b
				lo, hi = math.Min(lo, r.vals[i]), math.Max(hi, r.vals[i])
			} else {
				r.fail[i] = "n/a"
			}
		}
		rows = append(rows, r)
	}
	// Geomean row.
	gr := row{name: "geometric mean", vals: make([]float64, len(series)), fail: make([]string, len(series))}
	for i, s := range series {
		if g, n := geomean(m, progs, s, get); n > 0 {
			gr.vals[i] = g
			lo, hi = math.Min(lo, g), math.Max(hi, g)
		} else {
			gr.fail[i] = "n/a"
		}
	}
	lo = math.Max(math.Pow(2, math.Floor(math.Log2(lo))), 1.0/64)
	hi = math.Min(math.Pow(2, math.Ceil(math.Log2(hi))), 64)
	if lo == 1 {
		lo = 0.5
	}
	if hi == 1 {
		hi = 2
	}

	const (
		W       = 1200
		left    = 190
		right   = 80
		top     = 120
		rowH    = 26
		barH    = 9
		bottom  = 70
		fontFam = "Helvetica, Arial, sans-serif"
	)
	n := len(rows) + 2
	H := top + n*rowH + bottom
	plotW := float64(W - left - right)
	x := func(r float64) float64 {
		t := (math.Log2(r) - math.Log2(lo)) / (math.Log2(hi) - math.Log2(lo))
		return float64(left) + t*plotW
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" font-family="%s">`, W, H, W, H, fontFam)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#ffffff"/>`, W, H)
	fmt.Fprintf(&b, `<text x="%d" y="44" font-size="28" font-weight="700" fill="#111827">marmot: %s</text>`, 30, esc(m.Title))
	fmt.Fprintf(&b, `<text x="30" y="70" font-size="14" fill="#4b5563">Per program, ratio to %s on a log scale. %s. %s.</text>`, esc(m.Base), esc(meta.CPU), esc(meta.Date))
	// Legend.
	lx := 30.0
	for _, s := range series {
		fmt.Fprintf(&b, `<rect x="%.0f" y="86" width="14" height="14" rx="2" fill="%s"/><text x="%.0f" y="98" font-size="14" fill="#111827">%s</text>`, lx, palette[s], lx+20, esc(s))
		lx += 40 + float64(len(s))*8
	}
	// Grid and axis ticks.
	for t := lo; t <= hi*1.0001; t *= 2 {
		xx := x(t)
		col, wd := "#e5e7eb", 1.0
		if t == 1 {
			col, wd = "#374151", 2
		}
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%d" x2="%.1f" y2="%d" stroke="%s" stroke-width="%.0f"/>`, xx, top-8, xx, top+n*rowH, col, wd)
		fmt.Fprintf(&b, `<text x="%.1f" y="%d" font-size="12" fill="#4b5563" text-anchor="middle">%sx</text>`, xx, top+n*rowH+18, trimNum(t))
	}
	fmt.Fprintf(&b, `<text x="%.1f" y="%d" font-size="13" fill="#065f46" text-anchor="end">better</text>`, x(1)-8, top-14)
	fmt.Fprintf(&b, `<text x="%.1f" y="%d" font-size="13" fill="#991b1b">worse</text>`, x(1)+8, top-14)
	fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="13" fill="#374151" text-anchor="middle">ratio to %s (1x = same as %s)</text>`, left+int(plotW)/2, top+n*rowH+40, esc(m.Base), esc(m.Base))

	drawRow := func(i int, r row, bold bool) {
		y := float64(top + i*rowH)
		w := "400"
		if bold {
			w = "700"
			fmt.Fprintf(&b, `<line x1="20" y1="%.1f" x2="%d" y2="%.1f" stroke="#9ca3af"/>`, y+2, W-20, y+2)
		}
		fmt.Fprintf(&b, `<text x="%d" y="%.1f" font-size="14" font-weight="%s" fill="#111827" text-anchor="end">%s</text>`, left-12, y+rowH/2+4, w, esc(r.name))
		for k := range series {
			yy := y + 4 + float64(k)*(barH+2)
			if r.vals[k] == 0 {
				fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="10" fill="#9ca3af">%s: %s</text>`, x(1)+6, yy+8, esc(series[k]), failText(r.fail[k]))
				continue
			}
			v := r.vals[k]
			cv := math.Min(math.Max(v, lo), hi)
			x0, x1 := x(1), x(cv)
			if x1 < x0 {
				x0, x1 = x1, x0
			}
			fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%d" fill="%s"/>`, x0, yy, math.Max(x1-x0, 1.5), barH, palette[series[k]])
			anchor, tx := "start", x(cv)+5
			if v < 1 {
				anchor, tx = "end", x(cv)-5
			}
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="10" fill="#111827" text-anchor="%s">%.2fx</text>`, tx, yy+8, anchor, v)
		}
	}
	i := 0
	prev := ""
	for _, r := range rows {
		_ = prev
		drawRow(i, r, false)
		i++
	}
	_ = tier
	i++
	drawRow(i, gr, true)
	fmt.Fprintf(&b, `<text x="30" y="%d" font-size="12" fill="#6b7280">Losses are shown, not hidden. Programs that failed to build or print the right output on a toolchain are marked n/a. Source: bench/results, %s.</text>`, H-14, esc(meta.GitRev))
	b.WriteString("</svg>\n")
	return b.String()
}

func failText(s string) string {
	if s == "" {
		return "n/a"
	}
	return s
}

func trimNum(t float64) string {
	if t < 1 {
		return fmt.Sprintf("1/%.0f", 1/t)
	}
	return fmt.Sprintf("%.0f", t)
}

func esc(s string) string { return html.EscapeString(s) }

func rel(p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}

func findRoot() string {
	d, _ := os.Getwd()
	for ; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "SPEC.md")); err == nil {
			return d
		}
	}
	fatal("cannot find repo root")
	return ""
}

func check(err error) {
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(s string) {
	fmt.Fprintln(os.Stderr, "report:", s)
	os.Exit(1)
}
