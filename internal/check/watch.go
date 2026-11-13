package check

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

// watchPoll is how often the module trees are compared, and watchSettle
// how long they must stay unchanged before a run (an editor saving
// several files, a formatter rewriting the file it saved).
const (
	watchPoll   = 300 * time.Millisecond
	watchSettle = 150 * time.Millisecond
)

// Watch runs the checks, then again after every change to the files of
// the module trees, and reports only the findings that are new since the
// previous run and those that went away. The caches make each run
// incremental: only the packages an edit reaches are analyzed again. It
// returns when interrupted.
func Watch(opts Options, rep *diag.Reporter, details bool) int {
	dir, err := filepath.Abs(cmpOr(opts.Dir, "."))
	if err != nil {
		return rep.Usage("marmot check: " + err.Error())
	}
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	opts.Dir = dir
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)

	sig := func() string {
		sc, err := makeScope(dir, opts, openStore(opts.CacheDir))
		if err != nil {
			return "error: " + err.Error()
		}
		lc, _ := findLintConfig(dir)
		return runKey(sc, opts, lc, opts.Only)
	}
	var prev map[string][]diag.Diagnostic
	last := ""
	for round := 0; ; round++ {
		last = sig()
		start := time.Now()
		res, err := Run(opts)
		if err != nil {
			fmt.Fprintln(os.Stderr, "marmot check:", err)
		} else {
			cur := findingSet(res)
			if round == 0 {
				switch rep.Mode {
				case diag.ModeJSON:
					writeCheckJSON(rep, res, start, 0)
				default:
					writeCheckText(os.Stdout, rep, res, details, false, time.Since(start))
					where := diag.RelPath(rep.Dir, dir)
					if dir == rep.Dir {
						where = "."
					}
					fmt.Fprintf(os.Stdout, "\nwatching %s for changes (ctrl-c to stop)\n", where)
				}
			} else {
				added, gone := diffFindings(prev, cur)
				writeWatchRound(rep, res.Dir, added, gone, total(cur), time.Since(start), details)
			}
			prev = cur
		}
		// Wait for a change, then for the tree to settle.
		for {
			select {
			case <-stop:
				return 0
			case <-time.After(watchPoll):
			}
			s := sig()
			if s == last {
				continue
			}
			for {
				select {
				case <-stop:
					return 0
				case <-time.After(watchSettle):
				}
				s2 := sig()
				if s2 == s {
					break
				}
				s = s2
			}
			break
		}
	}
}

// findingKey identifies a finding across runs without its line, which an
// edit above it moves.
func findingKey(d *diag.Diagnostic) string {
	return d.Check + "\x00" + d.Analyzer + "\x00" + d.Span.File + "\x00" + d.Message
}

func findingSet(res *Result) map[string][]diag.Diagnostic {
	m := map[string][]diag.Diagnostic{}
	for _, c := range res.Checks {
		for _, d := range c.Findings {
			k := findingKey(&d)
			m[k] = append(m[k], d)
		}
	}
	return m
}

func total(m map[string][]diag.Diagnostic) int {
	n := 0
	for _, ds := range m {
		n += len(ds)
	}
	return n
}

// diffFindings returns the findings of cur beyond those of prev with the
// same key, and those of prev that cur no longer has, in file order.
func diffFindings(prev, cur map[string][]diag.Diagnostic) (added, gone []diag.Diagnostic) {
	for k, ds := range cur {
		if n := len(prev[k]); len(ds) > n {
			added = append(added, ds[n:]...)
		}
	}
	for k, ds := range prev {
		if n := len(cur[k]); len(ds) > n {
			gone = append(gone, ds[n:]...)
		}
	}
	order := func(ds []diag.Diagnostic) {
		sort.Slice(ds, func(i, j int) bool {
			a, b := ds[i].Span, ds[j].Span
			if a.File != b.File {
				return a.File < b.File
			}
			return a.Line < b.Line || a.Line == b.Line && a.Col < b.Col
		})
	}
	order(added)
	order(gone)
	return added, gone
}

func writeWatchRound(rep *diag.Reporter, dir string, added, gone []diag.Diagnostic, findings int, took time.Duration, details bool) {
	if rep.Mode == diag.ModeJSON {
		diag.WriteJSON(rep.W, added)
		enc := json.NewEncoder(rep.W)
		enc.SetEscapeHTML(false)
		for i := range gone {
			d := gone[i]
			_ = enc.Encode(struct {
				Kind     string    `json:"kind"`
				Schema   int       `json:"schema"`
				Check    string    `json:"check"`
				Analyzer string    `json:"analyzer,omitempty"`
				Span     diag.Span `json:"span"`
				Message  string    `json:"message"`
			}{"resolved", diag.Schema, d.Check, d.Analyzer, d.Span, d.Message})
		}
		_ = enc.Encode(map[string]any{"kind": "watch", "schema": diag.Schema, "new": len(added), "resolved": len(gone), "findings": findings, "durationMs": took.Milliseconds()})
		return
	}
	writeWatchText(os.Stdout, rep, dir, added, gone, findings, took, details)
}

func writeWatchText(w io.Writer, rep *diag.Reporter, dir string, added, gone []diag.Diagnostic, findings int, took time.Duration, details bool) {
	st := diag.Plain
	if rep.Color {
		st = diag.Colors
	}
	stamp := time.Now().Format("15:04:05")
	if len(added) == 0 && len(gone) == 0 {
		fmt.Fprintf(w, "%s%s no new findings (%d in all, %.2fs)%s\n", st.Dim, stamp, findings, took.Seconds(), st.Reset)
		return
	}
	fmt.Fprintf(w, "%s%s %d new, %d resolved (%d in all, %.2fs)%s\n", st.Bold, stamp, len(added), len(gone), findings, took.Seconds(), st.Reset)
	h := &diag.Human{W: w, Style: st, Dir: dir}
	for i := range added {
		if details {
			h.RenderOne(&added[i])
			fmt.Fprintln(w)
			continue
		}
		writeWrapped(w, findingLine(dir, &added[i]), st.Error+"+"+st.Reset+" ", "    ")
	}
	for i := range gone {
		writeWrapped(w, findingLine(dir, &gone[i]), "- ", "    ")
	}
}
