package check

import (
	"slices"
	"go/ast"
	"go/token"
	"strings"
)

// nolintRanges finds the //nolint directives of a file, with the ranges
// golangci-lint gives them: the line of the comment, extended to the end
// of a node that starts on the next line at the same column (a directive
// on its own line above a declaration or statement covers all of it).
func nolintRanges(fset *token.FileSet, f *ast.File) []nolintRange {
	type pending struct {
		r   nolintRange
		col int
	}
	var ps []pending
	name := fset.File(f.FileStart).Name()
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			linters, ok := parseNolint(c.Text)
			if !ok {
				continue
			}
			pos := fset.Position(c.Slash)
			ps = append(ps, pending{nolintRange{File: name, From: pos.Line, To: pos.Line, Linters: linters}, pos.Column})
		}
	}
	if len(ps) == 0 {
		return nil
	}
	out := make([]nolintRange, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.r)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		start := fset.Position(n.Pos())
		for _, p := range ps {
			if p.r.To == start.Line-1 && start.Column == p.col {
				r := p.r
				if end := fset.Position(n.End()).Line; end > r.To {
					r.To = end
				}
				out = append(out, r)
			}
		}
		return true
	})
	return out
}

// parseNolint reads a //nolint or //nolint:a,b comment. It returns the
// linter names (nil for all) and whether the comment is a directive.
func parseNolint(text string) ([]string, bool) {
	t := strings.TrimPrefix(text, "//")
	if t == text {
		return nil, false
	}
	t = strings.TrimLeft(t, " ")
	if !strings.HasPrefix(t, "nolint") {
		return nil, false
	}
	t = t[len("nolint"):]
	if t == "" || t[0] == ' ' || t[0] == '/' {
		return nil, true
	}
	if t[0] != ':' {
		return nil, false
	}
	t = t[1:]
	if i := strings.IndexAny(t, " /"); i >= 0 {
		t = t[:i]
	}
	var names []string
	for n := range strings.SplitSeq(t, ",") {
		n = strings.TrimSpace(n)
		if n == "all" {
			return nil, true
		}
		if n != "" {
			names = append(names, n)
		}
	}
	return names, true
}

// suppressed reports whether a finding of the linter (or analyzer) at the
// line is covered by a directive.
func suppressed(rs []nolintRange, file string, line int, names ...string) bool {
	for _, r := range rs {
		if r.File != file || line < r.From || line > r.To {
			continue
		}
		if len(r.Linters) == 0 {
			return true
		}
		for _, l := range r.Linters {
			if slices.Contains(names, l) {
					return true
				}
		}
	}
	return false
}
