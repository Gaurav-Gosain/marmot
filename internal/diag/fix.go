package diag

import (
	"fmt"
	"os"
	"sort"
)

// Applied is one fix written to disk.
type Applied struct {
	File  string `json:"file"`
	Code  string `json:"code"`
	Title string `json:"title"`
	Line  int    `json:"line"`
}

// ApplyFixes writes the fixes of the diagnostics to their files. When codes
// is not empty only diagnostics with one of those codes are fixed. Edits that
// overlap an edit already taken are skipped, so the result always parses as
// well as the input did. It returns what it changed.
func ApplyFixes(ds []Diagnostic, codes map[string]bool) ([]Applied, error) {
	type edit struct {
		a, b int
		text string
	}
	type fileEdits struct {
		f     *srcFile
		edits []edit
	}
	files := map[string]*fileEdits{}
	src := NewSources()
	var applied []Applied
	for _, d := range ds {
		if len(d.Fixes) == 0 || len(codes) > 0 && !codes[d.Code] {
			continue
		}
		fx := d.Fixes[0]
		// All edits of a fix go in, or none.
		type pending struct {
			fe *fileEdits
			e  edit
		}
		var ps []pending
		ok := true
		for _, e := range fx.Edits {
			fe := files[e.File]
			if fe == nil {
				f := src.file(e.File)
				if f == nil {
					ok = false
					break
				}
				fe = &fileEdits{f: f}
				files[e.File] = fe
			}
			a := fe.f.offset(e.Span.Line, e.Span.Col)
			b := a
			if e.Span.EndLine > 0 {
				b = fe.f.offset(e.Span.EndLine, e.Span.EndCol)
			}
			if a < 0 || b < a {
				ok = false
				break
			}
			for _, x := range fe.edits {
				if a < x.b && x.a < b || a == b && a == x.a {
					ok = false
				}
			}
			ps = append(ps, pending{fe, edit{a, b, e.NewText}})
		}
		if !ok {
			continue
		}
		for _, p := range ps {
			p.fe.edits = append(p.fe.edits, p.e)
		}
		applied = append(applied, Applied{File: d.Span.File, Code: d.Code, Title: fx.Title, Line: d.Span.Line})
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fe := files[n]
		if len(fe.edits) == 0 {
			continue
		}
		sort.Slice(fe.edits, func(i, j int) bool { return fe.edits[i].a > fe.edits[j].a })
		data := append([]byte(nil), fe.f.data...)
		for _, e := range fe.edits {
			data = append(data[:e.a:e.a], append([]byte(e.text), data[e.b:]...)...)
		}
		st, err := os.Stat(n)
		if err != nil {
			return applied, err
		}
		if err := os.WriteFile(n, data, st.Mode().Perm()); err != nil {
			return applied, fmt.Errorf("write %s: %v", n, err)
		}
	}
	return applied, nil
}
