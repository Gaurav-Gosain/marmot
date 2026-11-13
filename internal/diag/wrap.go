package diag

import (
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"
)

// Wrapping of human output: messages, labels, notes and help wrap at word
// boundaries to the width (the terminal's, at most maxWidth), and each
// continuation line starts under the text it continues. A backticked code
// span is never broken when it fits on a line of its own; a single word or
// span longer than a whole line is left long. Source lines are not wrapped.

// maxWidth caps the wrapping width: prose is hard to read in wider lines.
const maxWidth = 100

// TermWidth is the width human output of f wraps to: the terminal's (or
// $COLUMNS), at most maxWidth; maxWidth when unknown.
func TermWidth(f *os.File) int {
	w := 0
	if f != nil && IsTerminal(f) {
		if cols, _, err := term.GetSize(f.Fd()); err == nil {
			w = cols
		}
	}
	if w <= 0 {
		w, _ = strconv.Atoi(os.Getenv("COLUMNS"))
	}
	if w <= 0 || w > maxWidth {
		w = maxWidth
	}
	return w
}

// WidthOf is TermWidth of w when it is a file, else maxWidth.
func WidthOf(w io.Writer) int {
	if f, ok := w.(*os.File); ok {
		return TermWidth(f)
	}
	return maxWidth
}

// width is the wrapping width of h: Width (maxWidth when 0, at most
// maxWidth), or 0 for no wrapping when Width is negative.
func (h *Human) width() int {
	switch {
	case h.Width < 0:
		return 0
	case h.Width == 0 || h.Width > maxWidth:
		return maxWidth
	}
	return h.Width
}

// visible is the number of columns s takes (it has no escapes).
func visible(s string) int { return utf8.RuneCountInString(s) }

// words splits prose into the units wrapping keeps whole: runs of
// non-space characters, where a backticked span, a brace group (struct {
// X int }) and a func(...) group with its result type (func(int) error)
// count as one unit even when they hold spaces.
func words(text string) []string {
	var out []string
	var cur strings.Builder
	inCode := false
	depth := 0 // open braces, and parens of a func(...)
	funcEnd := false
	resultParen := false // the next ( opens a func's result list
	rs := []rune(text)
	for i, r := range rs {
		switch {
		case r == '`':
			inCode = !inCode
		case inCode:
		case r == '{':
			depth++
		case r == '}' && depth > 0:
			depth--
		case r == '(' && (depth > 0 || resultParen || strings.HasSuffix(cur.String(), "func")):
			depth++
			resultParen = false
		case r == ')' && depth > 0:
			depth--
			if depth == 0 && strings.Contains(cur.String(), "func(") {
				funcEnd = true
			}
		case r == ' ' && depth == 0:
			// After func(...): a simple result type joins the unit.
			if funcEnd && i+1 < len(rs) && resultStart(rs[i+1:]) {
				funcEnd = false
				resultParen = rs[i+1] == '('
				cur.WriteRune(r)
				continue
			}
			funcEnd = false
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// resultStart reports whether rs starts a Go result type right after a
// func(...) (a type name, *T, []T, map[, chan, or a result list).
func resultStart(rs []rune) bool {
	w := string(rs)
	if i := strings.IndexAny(w, " ,;)"); i >= 0 {
		w = w[:i]
	}
	switch {
	case w == "":
		return false
	case strings.HasPrefix(w, "*") || strings.HasPrefix(w, "[]") || strings.HasPrefix(w, "map[") || w == "chan" || strings.HasPrefix(w, "("):
		return true
	}
	// A type name: an identifier or pkg.Name, not a prose word.
	for _, t := range []string{"error", "bool", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8",
		"uint16", "uint32", "uint64", "uintptr", "byte", "rune", "float32", "float64", "complex64", "complex128", "any"} {
		if w == t {
			return true
		}
	}
	return strings.Contains(w, ".") || w != "" && w[0] >= 'A' && w[0] <= 'Z'
}

// wrapLines breaks text into lines of at most first columns (the first
// line) and rest columns (the others). width 0 means no wrapping.
func wrapLines(text string, first, rest int, wrap bool) []string {
	if !wrap || first <= 0 || strings.Contains(text, "\n") {
		return []string{text}
	}
	ws := words(text)
	if len(ws) == 0 {
		return []string{text}
	}
	var lines []string
	var cur strings.Builder
	avail := first
	n := 0
	for _, w := range ws {
		wl := visible(w)
		switch {
		case n == 0:
			cur.WriteString(w)
			n = wl
		case n+1+wl <= avail:
			cur.WriteString(" " + w)
			n += 1 + wl
		default:
			lines = append(lines, cur.String())
			cur.Reset()
			cur.WriteString(w)
			n = wl
			avail = max(rest, 1)
		}
	}
	return append(lines, cur.String())
}

// emit writes prefix + text, wrapped: the first line after prefix (which
// takes cols columns), the others after cont (also cols columns wide, a
// gutter or plain spaces). on and off wrap each line's text (color).
func (h *Human) emit(prefix string, cols int, cont, text, on, off string) {
	w := h.width()
	lines := wrapLines(text, w-cols, w-cols, w > 0)
	for i, l := range lines {
		p := prefix
		if i > 0 {
			p = cont
		}
		_, _ = io.WriteString(h.W, p+on+l+off+"\n") // output to the user; the other lines of Human ignore write errors too
	}
}
