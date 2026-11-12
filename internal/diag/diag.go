// Package diag is the one diagnostic model marmot uses for everything it
// reports: go list errors, parse and type errors, build and link failures,
// internal compiler errors and runtime panics. It parses the plain text the
// loader and driver produce, adds source context and suggestions, and renders
// the result for a person (human mode) or for a program (JSON Lines).
//
// All of the work happens on the failure path. A build with no errors never
// calls into this package beyond choosing the output mode.
package diag

import (
	"sort"
	"strings"
)

// Schema is the version of the JSON output. Bump it on incompatible changes.
const Schema = 1

// Severity of a diagnostic.
type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
	Note    Severity = "note"
)

// Span is a source range. Lines and columns are 1-based; columns count bytes,
// as go/token does. EndCol is exclusive. A span with an empty File has no
// location.
type Span struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	EndLine int    `json:"endLine"`
	EndCol  int    `json:"endCol"`
}

// Valid reports whether the span points into a file.
func (s Span) Valid() bool { return s.File != "" && s.Line > 0 }

// Label is a secondary span with a short text, drawn under the source line.
type Label struct {
	Span Span   `json:"span"`
	Text string `json:"text"`
}

// Edit replaces the text of Span with NewText. An empty span (EndLine:EndCol
// equal to Line:Col) inserts.
type Edit struct {
	File    string `json:"file"`
	Span    Span   `json:"span"`
	NewText string `json:"newText"`
}

// Fix is a machine applicable change: applying all of its edits resolves the
// diagnostic without other changes.
type Fix struct {
	Title string `json:"title"`
	Edits []Edit `json:"edits"`
}

// Related is another location that matters for the diagnostic, for example
// the declaration of a function whose call is wrong.
type Related struct {
	Span    Span   `json:"span"`
	Message string `json:"message"`
}

// SourceLine is one line of source with its number.
type SourceLine struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Context is what an agent would otherwise open files for. It is only filled
// in agent mode.
type Context struct {
	Function string       `json:"function,omitempty"` // enclosing function signature
	Lines    []SourceLine `json:"lines,omitempty"`    // source around the span
	Types    []string     `json:"types,omitempty"`    // one line definitions of named types
}

// Diagnostic is one reported problem.
type Diagnostic struct {
	Kind     string    `json:"kind"` // always "diagnostic"
	Severity Severity  `json:"severity"`
	Code     string    `json:"code"`
	Message  string    `json:"message"`
	Span     Span      `json:"span"`
	Labels   []Label   `json:"labels,omitempty"`
	Notes    []string  `json:"notes,omitempty"`
	Help     []string  `json:"help,omitempty"`
	Fixes    []Fix     `json:"fixes,omitempty"`
	Related  []Related `json:"related,omitempty"`
	DocURL   string    `json:"docURL,omitempty"`
	Context  *Context  `json:"context,omitempty"`

	// Primary is the text drawn under the carets of the main span.
	Primary string `json:"-"`
	// Bundle is the path of an internal error report bundle.
	Bundle string `json:"bundle,omitempty"`
	// Package is the import path the diagnostic belongs to, when known.
	Package string `json:"package,omitempty"`
}

// Sort orders diagnostics by file, line and column, with diagnostics without
// a location last in their original order. The order is the same every run.
func Sort(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i].Span, ds[j].Span
		if a.Valid() != b.Valid() {
			return a.Valid()
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Col != b.Col {
			return a.Col < b.Col
		}
		return ds[i].Message < ds[j].Message
	})
}

// Dedup drops diagnostics with the same location and message, and type
// errors in a file that already has a syntax error (they are follow-on
// errors of the broken parse).
func Dedup(ds []Diagnostic) []Diagnostic {
	syntax := map[string]bool{}
	for _, d := range ds {
		if d.Code == CodeSyntax && d.Span.File != "" {
			syntax[d.Span.File] = true
		}
	}
	type key struct {
		f    string
		l, c int
		m    string
	}
	seen := map[key]bool{}
	seenSyntax := map[string]bool{}
	var out []Diagnostic
	for _, d := range ds {
		if syntax[d.Span.File] && d.Code != CodeSyntax {
			continue
		}
		if d.Code == CodeSyntax && strings.Contains(d.Message, "found 'EOF'") && seenSyntax[d.Span.File] {
			// The parser ran to the end of the file after the first error.
			continue
		}
		if d.Code == CodeSyntax {
			seenSyntax[d.Span.File] = true
		}
		k := key{d.Span.File, d.Span.Line, d.Span.Col, d.Message}
		if d.Code == CodeSyntax {
			// A broken parse often reports the same token several times.
			k.m = ""
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, d)
	}
	return out
}

// Counts returns the number of errors and warnings.
func Counts(ds []Diagnostic) (errs, warns int) {
	for _, d := range ds {
		switch d.Severity {
		case Error:
			errs++
		case Warning:
			warns++
		}
	}
	return
}
