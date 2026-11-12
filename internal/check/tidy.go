package check

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

type tidyResult struct {
	Diff string
	Err  string
}

// runTidy runs go mod tidy -diff in every module of the scope (in Changed
// mode, the modules whose Go files or go.mod changed). A module whose
// go.mod, go.sum and Go files are unchanged since the last run is answered
// from the cache.
func runTidy(sc *scope, st *store, r *CheckResult) {
	for _, m := range sc.modules {
		if sc.changed != nil && !sc.moduleTouched(&m) {
			continue
		}
		key := tidyKey(sc, &m)
		var tr tidyResult
		if c := st.get(key); c != nil && gob.NewDecoder(bytes.NewReader(c)).Decode(&tr) == nil {
		} else {
			cmd := exec.Command("go", "mod", "tidy", "-diff")
			cmd.Dir = m.Dir
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
			var out, errb bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &errb
			err := cmd.Run()
			tr = tidyResult{Diff: out.String()}
			if err != nil && out.Len() == 0 {
				tr.Err = strings.TrimSpace(errb.String())
				if tr.Err == "" {
					tr.Err = err.Error()
				}
			}
			// Errors (no network, a missing commit) are not cached: they
			// depend on more than the files.
			if tr.Err == "" {
				var buf bytes.Buffer
				if gob.NewEncoder(&buf).Encode(tr) == nil {
					st.put(key, buf.Bytes())
				}
			}
		}
		gomod := filepath.Join(m.Dir, "go.mod")
		switch {
		case tr.Err != "":
			r.Status = Failed
			r.Notes = append(r.Notes, fmt.Sprintf("%s: go mod tidy: %s", diag.RelPath(sc.dir, gomod), firstLine(tr.Err)))
		case tr.Diff != "":
			d := diag.Diagnostic{
				Kind: "diagnostic", Severity: diag.Warning, Code: diag.CodeNotTidy, Check: "tidy", Analyzer: "go mod tidy",
				Message: "go.mod and go.sum are not tidy",
				Span:    diag.Span{File: gomod, Line: 1, Col: 1, EndLine: 1, EndCol: 1},
				Help:    []string{"run `go mod tidy` in " + diag.RelPath(sc.dir, m.Dir)},
			}
			for _, l := range strings.Split(strings.TrimRight(tr.Diff, "\n"), "\n") {
				if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") {
					d.Notes = append(d.Notes, l)
				}
			}
			r.Findings = append(r.Findings, d)
		}
	}
}

// tidyKey covers what go mod tidy reads: go.mod, go.sum and the imports
// of the module's Go files (approximated by their names, sizes and times).
func tidyKey(sc *scope, m *module) string {
	h := newHasher("tidy")
	h.str(toolID(), envKey(nil, hostGOOS(), hostGOARCH()), m.Dir)
	for _, n := range []string{"go.mod", "go.sum"} {
		data, _ := os.ReadFile(filepath.Join(m.Dir, n))
		h.bytes(data)
	}
	filepath.WalkDir(m.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != m.Dir {
				if strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_") || d.Name() == "testdata" || d.Name() == "vendor" {
					return filepath.SkipDir
				}
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			if info, err := d.Info(); err == nil {
				h.str(p, fmt.Sprint(info.Size(), info.ModTime().UnixNano()))
			}
		}
		return nil
	})
	return h.sum()
}

// moduleTouched reports whether a Changed run reaches the module.
func (sc *scope) moduleTouched(m *module) bool {
	for f := range sc.changed {
		if sc.moduleOf(f) != m {
			continue
		}
		b := filepath.Base(f)
		if strings.HasSuffix(f, ".go") || b == "go.mod" || b == "go.sum" {
			return true
		}
	}
	return false
}
