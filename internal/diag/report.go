package diag

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Reporter turns a failed command into output in the chosen mode.
type Reporter struct {
	Settings
	W       io.Writer
	Dir     string // working directory: relative paths resolve against it
	Command string // build, run, test, ...
	Start   time.Time
	// Version and Args go into internal error bundles.
	Version string
	Args    []string
}

// Diagnose parses a build error into diagnostics, enriched, deduplicated
// and sorted. Internal errors get a report bundle.
func (r *Reporter) Diagnose(err error) []Diagnostic {
	text := err.Error()
	ds := FromText(text, r.Dir)
	ds = Dedup(ds)
	Sort(ds)
	src := NewSources()
	src.Enrich(ds, Options{Context: r.Mode == ModeJSON})
	for i := range ds {
		if ds[i].Code == CodeInternal {
			r.internal(&ds[i], text)
		}
	}
	return ds
}

// Fail reports a failed build and returns the exit code (1).
func (r *Reporter) Fail(err error) int {
	switch r.Mode {
	case ModePlain:
		fmt.Fprintln(r.W, err)
	case ModeHuman:
		ds := r.Diagnose(err)
		st := Plain
		if r.Color {
			st = Colors
		}
		h := &Human{W: r.W, Style: st, Dir: r.Dir}
		h.Render(ds)
	case ModeJSON:
		ds := r.Diagnose(err)
		WriteJSON(r.W, ds)
		e, w := Counts(ds)
		if e == 0 {
			e = 1
		}
		WriteResult(r.W, Result{Command: r.Command, OK: false, Errors: e, Warnings: w, DurationMs: r.ms(), ExitCode: 1})
	}
	return 1
}

// OK reports success. Only JSON mode prints anything.
func (r *Reporter) OK(output string) {
	if r.Mode == ModeJSON {
		if output != "" && !filepath.IsAbs(output) {
			output = filepath.Join(r.Dir, output)
		}
		WriteResult(r.W, Result{Command: r.Command, OK: true, Output: output, DurationMs: r.ms()})
	}
}

// Usage reports a command line error (exit code 2).
func (r *Reporter) Usage(msg string) int {
	switch r.Mode {
	case ModeJSON:
		d := Diagnostic{Kind: "diagnostic", Severity: Error, Code: CodeUsage, Message: msg}
		WriteJSON(r.W, []Diagnostic{d})
		WriteResult(r.W, Result{Command: r.Command, OK: false, Errors: 1, DurationMs: r.ms(), ExitCode: 2})
	case ModeHuman:
		st := Plain
		if r.Color {
			st = Colors
		}
		fmt.Fprintf(r.W, "%serror:%s %s\n", st.Error, st.Reset, msg)
	default:
		fmt.Fprintln(r.W, msg)
	}
	return 2
}

func (r *Reporter) ms() int64 {
	if r.Start.IsZero() {
		return 0
	}
	return time.Since(r.Start).Milliseconds()
}

var reFailedFunc = lazy(`(?i)(?:in function|function|func)[: ]+['"@]*([^\s'"@]+)`)

// internal shortens an internal compiler error to one line, names the
// failing function when the tool output has it, and writes a report bundle.
func (r *Reporter) internal(d *Diagnostic, full string) {
	if strings.Contains(full, "Undefined symbols") || strings.Contains(full, "undefined reference") {
		d.Code = CodeLink
		r.linkError(d, full)
		return
	}
	tool, _, _ := strings.Cut(d.Message, ":")
	tool = strings.TrimSuffix(strings.TrimSuffix(tool, " failed"), " timed out")
	fn := ""
	if m := reFailedFunc.FindStringSubmatch(full); m != nil {
		fn = m[1]
	}
	d.Message = "internal compiler error in " + tool + " (this is a marmot bug, not a problem in your code)"
	if fn != "" {
		d.Message += ", while compiling " + fn
	}
	bundle, err := r.writeBundle(full)
	if err == nil {
		d.Bundle = bundle
	}
	// The raw tool output goes into the bundle, not on the screen.
	d.Notes = nil
	d.Help = append(d.Help, "please report it with the bundle; as a workaround try -O 0 or MARMOT_TIER0=0")
}

var reUndefSym = lazy(`"_?([^"]+)", referenced from|undefined reference to [` + "`" + `']_?([^'` + "`" + `]+)'|undefined symbol: _?(\S+)`)

// linkError lists the missing symbols.
func (r *Reporter) linkError(d *Diagnostic, full string) {
	seen := map[string]bool{}
	var syms []string
	for _, m := range reUndefSym.FindAllStringSubmatch(full, -1) {
		s := m[1] + m[2] + m[3]
		if !seen[s] {
			seen[s] = true
			syms = append(syms, s)
		}
	}
	sort.Strings(syms)
	d.Message = "link failed: undefined symbols"
	if len(syms) > 0 {
		show := syms
		if len(show) > 8 {
			show = show[:8]
		}
		d.Message = fmt.Sprintf("link failed: %d undefined symbol(s): %s", len(syms), strings.Join(show, ", "))
		if len(syms) > len(show) {
			d.Message += ", ..."
		}
	}
	d.Notes = nil
	d.Help = append(d.Help, "a function without a body (assembly or a runtime hook) has no marmot implementation; if it is in the standard library, please report it")
}

// writeBundle writes the inputs of a failed build for a bug report.
func (r *Reporter) writeBundle(full string) (string, error) {
	dir, err := os.MkdirTemp("", "marmot-bug-")
	if err != nil {
		return "", err
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "MARMOT_") || k == "GOOS" || k == "GOARCH" || k == "GOFLAGS" || k == "GOAMD64" || k == "GOARM64" || k == "CGO_ENABLED" {
			env = append(env, kv)
		}
	}
	sort.Strings(env)
	info := fmt.Sprintf("version: %s\nhost: %s/%s\ngo: %s\ncommand: marmot %s\nenv:\n  %s\n",
		r.Version, runtime.GOOS, runtime.GOARCH, runtime.Version(), strings.Join(r.Args, " "), strings.Join(env, "\n  "))
	if err := os.WriteFile(filepath.Join(dir, "info.txt"), []byte(info), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "error.txt"), []byte(full), 0o644); err != nil {
		return "", err
	}
	// The source of the package being built, small files only.
	if ents, err := os.ReadDir(r.Dir); err == nil {
		os.Mkdir(filepath.Join(dir, "src"), 0o755)
		total := 0
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || !(strings.HasSuffix(n, ".go") || n == "go.mod" || n == "go.sum") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(r.Dir, n))
			if err != nil || total+len(data) > 4<<20 {
				continue
			}
			total += len(data)
			os.WriteFile(filepath.Join(dir, "src", n), data, 0o644)
		}
	}
	return dir, nil
}
