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

// Applicability says how safe a fix is to apply without looking (the rustc
// levels).
const (
	// MachineApplicable fixes are certainly what the code needs; marmot fix
	// applies them by default.
	MachineApplicable = "machine-applicable"
	// MaybeIncorrect fixes are likely but a person should check them (a
	// typo correction may pick the wrong name). marmot fix --all applies them.
	MaybeIncorrect = "maybe-incorrect"
	// HasPlaceholders fixes contain text to fill in; never applied.
	HasPlaceholders = "has-placeholders"
)

// Fix is a suggested change: applying all of its edits resolves the
// diagnostic.
type Fix struct {
	Title         string `json:"title"`
	Applicability string `json:"applicability"`
	Edits         []Edit `json:"edits"`
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

	// Primary is the text drawn under the carets of the main span. It must
	// make sense alone (editors show only it).
	Primary string `json:"label,omitempty"`
	// FullMessage is the message before long type names were shortened.
	FullMessage string `json:"fullMessage,omitempty"`
	// Rendered is the human form without color, as a person would see it.
	Rendered string `json:"rendered,omitempty"`
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
	// "could not import P" is a follow-on of the go list error for P at the
	// same import.
	type at struct {
		f    string
		l, c int
	}
	positions := map[at]int{}
	for _, d := range ds {
		positions[at{d.Span.File, d.Span.Line, d.Span.Col}]++
	}
	var out []Diagnostic
	for _, d := range ds {
		if syntax[d.Span.File] && d.Code != CodeSyntax {
			continue
		}
		if strings.HasPrefix(d.Message, "could not import ") && positions[at{d.Span.File, d.Span.Line, d.Span.Col}] > 1 {
			continue
		}
		if d.Code == CodeSyntax && seenSyntax[d.Span.File] {
			// After the first syntax error the parser is out of step; the
			// errors that follow are almost always caused by the first.
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

// containerCodes are errors about a whole expression that go/types also
// reports when a part of it is wrong; with an error inside their span they
// are a second error for one cause.
var containerCodes = map[string]bool{CodeAssignCount: true, CodeCannotUse: true, CodeMismatched: true, CodeInvalidOp: true, CodeOther: true}

// DropCascades removes an error whose span strictly contains the span of
// another error on the same line, when its code is one that go/types reports
// for a whole expression. The inner error is the cause.
func DropCascades(ds []Diagnostic) []Diagnostic {
	var out []Diagnostic
	for i, d := range ds {
		drop := false
		if containerCodes[d.Code] && d.Span.Valid() {
			a := d.Span
			for j, e := range ds {
				if i == j || !e.Span.Valid() || e.Span.File != d.Span.File || e.Span.Line != d.Span.Line {
					continue
				}
				b := e.Span
				if a.Col <= b.Col && b.EndCol <= a.EndCol && (a.Col != b.Col || a.EndCol != b.EndCol) {
					drop = true
				}
			}
		}
		if !drop {
			out = append(out, d)
		}
	}
	return out
}

// Options control how a failed build's text becomes diagnostics.
type Options struct {
	// Context fills Diagnostic.Context (agent mode).
	Context bool
	// ContextLines is the number of lines around the span in Context.
	ContextLines int
	// Rendered fills Diagnostic.Rendered with the human form (JSON mode).
	Rendered bool
	// Dir is the directory paths are shown relative to.
	Dir string
}

// Process turns the plain error text of a build into diagnostics: parse,
// drop duplicates and follow-on errors, enrich from the sources, drop
// cascades, sort, shorten long messages, and render when asked.
func Process(text string, src *Sources, opt Options) []Diagnostic {
	ds := Dedup(FromText(text, opt.Dir))
	return Finish(ds, src, opt)
}

// Finish runs the steps of Process after parsing.
func Finish(ds []Diagnostic, src *Sources, opt Options) []Diagnostic {
	src.Enrich(ds, opt)
	for i := range ds {
		relatedToLabels(&ds[i])
	}
	ds = DropCascades(ds)
	Sort(ds)
	for i := range ds {
		shortenMessage(&ds[i])
	}
	if opt.Rendered {
		for i := range ds {
			var b strings.Builder
			h := &Human{W: &b, Style: Plain, Dir: opt.Dir, Sources: src}
			h.one(&ds[i])
			ds[i].Rendered = b.String()
		}
	}
	return ds
}

// relatedToLabels draws related locations in the diagnostic's own file as
// labels in the snippet, the way rustc shows "first defined here".
func relatedToLabels(d *Diagnostic) {
	var keep []Related
	for _, r := range d.Related {
		if r.Span.File != d.Span.File || r.Span.File == "" {
			keep = append(keep, r)
			continue
		}
		sp := r.Span
		text := r.Message
		switch {
		case strings.HasPrefix(text, "other declaration of "):
			text = "`" + strings.TrimPrefix(text, "other declaration of ") + "` is first declared here"
		case text == "previous case":
			text = "first used here"
		}
		if sp.EndLine == 0 {
			sp.EndLine, sp.EndCol = sp.Line, sp.Col+1
			if n := strings.TrimPrefix(r.Message, "other declaration of "); n != r.Message {
				sp.EndCol = sp.Col + len(n)
			}
		}
		d.Labels = append(d.Labels, Label{Span: sp, Text: text})
	}
	d.Related = keep
}

// maxMessage is the longest message shown in full in human mode.
const maxMessage = 200

// shortenMessage elides the inside of long type names (brackets nested
// deeper than one level) when the message is too long. The full text stays
// in FullMessage.
func shortenMessage(d *Diagnostic) {
	if len(d.Message) <= maxMessage {
		return
	}
	var b strings.Builder
	depth := 0
	for _, r := range d.Message {
		switch r {
		case '(', '[', '{':
			depth++
			if depth == 2 {
				b.WriteRune(r)
				b.WriteString("...")
				continue
			}
		case ')', ']', '}':
			depth--
		}
		if depth >= 2 {
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() < len(d.Message) {
		d.FullMessage = d.Message
		d.Message = b.String()
	}
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
