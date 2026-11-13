package check

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

// defaultTargets are the cross build targets when the project names none.
var defaultTargets = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"}

// targets returns the cross build targets: the option, else a platforms
// file (scripts/platforms.txt or platforms.txt, one goos or goos/goarch per
// line, # comments), else the defaults. A goos alone means the host
// architecture.
func targets(sc *scope, opts Options) ([]string, string) {
	list, from := opts.Targets, ""
	if len(list) == 0 {
		for _, n := range []string{filepath.Join("scripts", "platforms.txt"), "platforms.txt"} {
			for _, d := range []string{sc.dir, workRoot(sc.dir)} {
				if d == "" {
					continue
				}
				f, err := os.Open(filepath.Join(d, n))
				if err != nil {
					continue
				}
				sc := bufio.NewScanner(f)
				for sc.Scan() {
					l := strings.TrimSpace(sc.Text())
					if i := strings.IndexByte(l, '#'); i >= 0 {
						l = strings.TrimSpace(l[:i])
					}
					if l != "" {
						list = append(list, l)
					}
				}
				f.Close()
				from = n
				break
			}
			if from != "" {
				break
			}
		}
	}
	if len(list) == 0 {
		list = defaultTargets
	}
	var out []string
	for _, t := range list {
		if !strings.Contains(t, "/") {
			t += "/" + hostGOARCH()
		}
		out = append(out, t)
	}
	return out, from
}

// runCross type checks the packages, without tests, for every target, as
// go build would compile them (no link). Each target has its own package
// graph and cache keys, so an unchanged target costs one cache lookup per
// package.
func runCross(sc *scope, opts Options, st *store, r *CheckResult) {
	if len(sc.patterns) == 0 {
		return
	}
	ts, from := targets(sc, opts)
	if from != "" {
		r.Notes = append(r.Notes, "targets from "+from+": "+strings.Join(ts, " "))
	} else {
		r.Notes = append(r.Notes, "targets: "+strings.Join(ts, " "))
	}
	s := newSuite(nil, toolID(), "")
	type out struct {
		ds    []diag.Diagnostic
		notes []string
	}
	res := make([]out, len(ts))
	// Targets one after another: each one type checks a whole program.
	var mu sync.Mutex
	for i, t := range ts {
		goos, goarch, _ := strings.Cut(t, "/")
		env := []string{"GOOS=" + goos, "GOARCH=" + goarch, "CGO_ENABLED=0"}
		g, _, err := cachedGoList(st, sc, opts.Tags, env, false)
		if err != nil {
			res[i].notes = append(res[i].notes, fmt.Sprintf("%s: %v", t, firstLine(err.Error())))
			r.Status = Failed
			continue
		}
		e := newEngine(g, st, s, envKey(opts.Tags, goos, goarch)+"|cgo0", goarch, opts.Jobs)
		var wg sync.WaitGroup
		for _, p := range g.roots {
			wg.Add(1)
			go func(p *listPkg) {
				defer wg.Done()
				rep, err := e.report(p)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					res[i].notes = append(res[i].notes, fmt.Sprintf("%s: %s: %v", t, p.ImportPath, firstLine(err.Error())))
					return
				}
				for _, le := range p.listErrors() {
					if strings.Contains(le, "build constraints exclude all Go files") {
						continue // a package for other systems
					}
					d := diagFromText(le, sc.dir)
					res[i].ds = append(res[i].ds, crossDiag(d, t))
				}
				for _, te := range rep.TypeErrors {
					d := diag.Diagnostic{Kind: "diagnostic", Severity: diag.Error, Message: te.Message, Span: te.Span, Related: te.Related}
					res[i].ds = append(res[i].ds, crossDiag(d, t))
				}
			}(p)
		}
		wg.Wait()
	}
	seen := map[string]bool{}
	for _, o := range res {
		r.Notes = append(r.Notes, o.notes...)
		for _, d := range o.ds {
			addOnce(r, d, seen)
		}
	}
}

func crossDiag(d diag.Diagnostic, target string) diag.Diagnostic {
	d.Kind, d.Check, d.Code, d.Analyzer = "diagnostic", "cross", diag.CodeCross, target
	d.Severity = diag.Error
	d.Notes = append(d.Notes, "when building for "+target)
	return d
}

func runDeadcode(sc *scope, opts Options, st *store, r *CheckResult) {
	r.Status = Skipped
	r.Notes = append(r.Notes, "not implemented yet")
}

func runVuln(sc *scope, opts Options, st *store, r *CheckResult) {
	r.Status = Skipped
	r.Notes = append(r.Notes, "not implemented yet")
}
