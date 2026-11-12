package diag

import (
	"path/filepath"
	"strconv"
	"strings"
)

// posRE matches the position prefix the go tool chain uses:
// file:line:col: message, or file:line: message.
var posRE = lazy(`^((?:[A-Za-z]:)?[^:\s][^:]*\.(?:go|s|c|h|mod)):(\d+)(?::(\d+))?: (.*)$`)

// FromText splits the plain text of a failed build into diagnostics. Each
// line that starts with a position, or a non-indented line, starts a new
// diagnostic; indented lines that follow belong to it. Relative paths are
// made absolute against dir.
func FromText(text, dir string) []Diagnostic {
	var ds []Diagnostic
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	internal := false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if internal {
			// Tool output after an internal error header stays with it.
			ds[len(ds)-1].Notes = append(ds[len(ds)-1].Notes, l)
			continue
		}
		if len(ds) > 0 && (strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "    ")) {
			d := &ds[len(ds)-1]
			d.Message += "\n" + l
			continue
		}
		d := Diagnostic{Kind: "diagnostic", Severity: Error}
		if m := posRE.FindStringSubmatch(l); m != nil {
			file := m[1]
			if !filepath.IsAbs(file) && dir != "" {
				file = filepath.Join(dir, file)
			}
			line, _ := strconv.Atoi(m[2])
			col, _ := strconv.Atoi(m[3])
			d.Span = Span{File: file, Line: line, Col: col}
			d.Message = m[4]
			if strings.HasPrefix(m[4], "\t") && len(ds) > 0 {
				// go/types continuation: another location of the error
				// above ("other declaration of x", "previous case").
				prev := &ds[len(ds)-1]
				prev.Related = append(prev.Related, Related{Span: d.Span, Message: strings.TrimSpace(m[4])})
				continue
			}
		} else {
			msg := l
			// go list prefixes position-less errors with "-: " or "pkg: ".
			msg = strings.TrimPrefix(msg, "-: ")
			d.Message = msg
		}
		d.Message = strings.TrimSpace(d.Message)
		d.Code = Classify(d.Message)
		if d.Code == CodeInternal {
			internal = true
		}
		ds = append(ds, d)
	}
	// Messages like "too many return values\n\thave (number)\n\twant ()"
	// keep the detail lines as notes.
	for i := range ds {
		if ds[i].Code == CodeInternal {
			continue
		}
		head, rest, ok := strings.Cut(ds[i].Message, "\n")
		if !ok {
			continue
		}
		ds[i].Message = head
		for _, r := range strings.Split(rest, "\n") {
			if r = strings.TrimSpace(r); r != "" {
				ds[i].Notes = append(ds[i].Notes, r)
			}
		}
	}
	return ds
}
