package diag

import (
	"path/filepath"
	"regexp"
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
var (
	clangCount   = regexp.MustCompile(`^\d+ (errors?|warnings?)( and \d+ (errors?|warnings?))? generated\.$`)
	clangExcerpt = regexp.MustCompile(`^\s*(\d*) \| ?(.*)$`)
	pkgPrefix    = regexp.MustCompile(`^[\w.\-]+(?:/[\w.\-]+)*: (\S+:\d+:\d+: .*)$`)
)

func FromText(text, dir string) []Diagnostic {
	var ds []Diagnostic
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	internal := false
	lastC := false // the previous diagnostic came from clang
	for i := range lines {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		// clang (cgo preamble, C files): "N errors generated." and the
		// excerpt under an error ("  4 | code", "    |   ^", "    |   ;").
		if clangCount.MatchString(l) {
			continue
		}
		if lastC && clangExcerpt.MatchString(l) {
			if m := clangExcerpt.FindStringSubmatch(l); m[1] == "" {
				// A caret line, then possibly clang's fix-it text.
				if t := strings.TrimSpace(strings.Trim(m[2], "^~ ")); t != "" && !strings.ContainsAny(m[2], "^") {
					d := &ds[len(ds)-1]
					d.Fixes = append(d.Fixes, Fix{Title: "insert `" + t + "`", Applicability: MachineApplicable,
						Edits: []Edit{{File: d.Span.File, Span: Span{File: d.Span.File, Line: d.Span.Line, Col: d.Span.Col, EndLine: d.Span.Line, EndCol: d.Span.Col}, NewText: t}}})
				}
			}
			continue
		}
		// The loader prefixes a package's errors with its path.
		if m := pkgPrefix.FindStringSubmatch(l); m != nil {
			l = m[1]
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
		lastC = false
		if d.Span.Valid() {
			switch {
			case strings.HasPrefix(d.Message, "error: "), strings.HasPrefix(d.Message, "fatal error: "):
				// clang's format: go/types never writes "error:".
				d.Message = strings.TrimPrefix(strings.TrimPrefix(d.Message, "fatal "), "error: ")
				d.Code, lastC = CodeC, true
			case strings.HasPrefix(d.Message, "warning: "):
				continue // C warnings do not fail the build
			case strings.HasPrefix(d.Message, "note: ") && len(ds) > 0:
				prev := &ds[len(ds)-1]
				prev.Related = append(prev.Related, Related{Span: d.Span, Message: strings.TrimPrefix(d.Message, "note: ")})
				lastC = true
				continue
			}
		}
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
		for r := range strings.SplitSeq(rest, "\n") {
			if r = strings.TrimSpace(r); r != "" {
				ds[i].Notes = append(ds[i].Notes, r)
			}
		}
	}
	return ds
}
