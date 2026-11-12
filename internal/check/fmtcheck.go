package check

import (
	"bytes"
	"encoding/gob"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
)

// fmtResult is the cached outcome for one file version.
type fmtResult struct {
	Formatted []byte // nil when the file is already formatted
	Err       string
}

// runFmt checks that every Go file is formatted as gofmt and goimports
// would leave it. In Changed mode only the changed files are read.
func runFmt(sc *scope, st *store, r *CheckResult) {
	files := sc.changedGo
	if sc.changed == nil {
		files = goFiles(sc)
	}
	src := diag.NewSources()
	type out struct {
		d  *diag.Diagnostic
		ok bool
	}
	res := make([]out, len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, f string) {
			defer wg.Done()
			defer func() { <-sem }()
			data, err := os.ReadFile(f)
			if err != nil {
				return
			}
			h := newHasher("fmt")
			h.str(toolID(), f)
			h.bytes(data)
			key := h.sum()
			var fr fmtResult
			if c := st.get(key); c != nil && gob.NewDecoder(bytes.NewReader(c)).Decode(&fr) == nil {
			} else {
				fr = fmtResult{}
				got, err := src.FormatFile(f, data)
				if err != nil {
					fr.Err = err.Error()
				} else if !bytes.Equal(got, data) {
					fr.Formatted = got
				}
				var buf bytes.Buffer
				if gob.NewEncoder(&buf).Encode(fr) == nil {
					st.put(key, buf.Bytes())
				}
			}
			if fr.Err != "" {
				d := diagFromText(fr.Err, filepath.Dir(f))
				if !d.Span.Valid() {
					d.Span = diag.Span{File: f, Line: 1, Col: 1, EndLine: 1, EndCol: 1}
				}
				d.Check, d.Code, d.Analyzer = "fmt", diag.CodeNotFmt, "gofmt"
				res[i] = out{d: &d}
				return
			}
			if fr.Formatted == nil {
				return
			}
			d := fmtDiag(f, data, fr.Formatted)
			res[i] = out{d: &d}
		}(i, f)
	}
	wg.Wait()
	for _, o := range res {
		if o.d != nil {
			r.Findings = append(r.Findings, *o.d)
		}
	}
}

// fmtDiag reports a file that is not formatted, with the first changed
// line as its position and the whole new text as one fix.
func fmtDiag(file string, old, new []byte) diag.Diagnostic {
	ol, nl := strings.Split(string(old), "\n"), strings.Split(string(new), "\n")
	line := 1
	for line <= len(ol) && line <= len(nl) && ol[line-1] == nl[line-1] {
		line++
	}
	what := "gofmt"
	if gofmtOnly(old) != nil && bytes.Equal(gofmtOnly(old), old) {
		what = "goimports"
	}
	lastLine := len(ol)
	lastCol := len(ol[lastLine-1]) + 1
	return diag.Diagnostic{
		Kind:     "diagnostic",
		Severity: diag.Warning,
		Code:     diag.CodeNotFmt,
		Check:    "fmt",
		Analyzer: what,
		Message:  "file is not formatted (" + what + ")",
		Span:     diag.Span{File: file, Line: line, Col: 1, EndLine: line, EndCol: 1},
		Fixes: []diag.Fix{{
			Title:         "format the file",
			Applicability: diag.MachineApplicable,
			Edits:         []diag.Edit{{File: file, Span: diag.Span{File: file, Line: 1, Col: 1, EndLine: lastLine, EndCol: lastCol}, NewText: string(new)}},
		}},
	}
}

// goFiles lists the Go files of the scope's modules: the files git tracks
// and the new ones it does not ignore, or every .go file outside hidden
// directories when the tree is not a git checkout.
func goFiles(sc *scope) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] && diag.IsGoFile(p) {
			seen[p] = true
			out = append(out, p)
		}
	}
	top := gitOut(sc.dir, "rev-parse", "--show-toplevel")
	if top != "" {
		ls := gitOut(sc.dir, "ls-files", "--full-name", "--cached", "--others", "--exclude-standard", "--", "*.go")
		for _, n := range strings.Split(ls, "\n") {
			if n == "" {
				continue
			}
			p := filepath.Join(top, n)
			if !strings.HasPrefix(p, sc.dir+string(os.PathSeparator)) {
				continue
			}
			if _, err := os.Stat(p); err == nil {
				add(p)
			}
		}
	} else {
		filepath.WalkDir(sc.dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && p != sc.dir && (strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_")) {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				add(p)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// gofmtOnly returns the source formatted by gofmt alone, or nil when it
// does not parse.
func gofmtOnly(src []byte) []byte {
	out, err := format.Source(src)
	if err != nil {
		return nil
	}
	return out
}
