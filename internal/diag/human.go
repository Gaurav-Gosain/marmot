package diag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/scanner"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Style holds the escape sequences of the human renderer. The palette uses
// the 16 base ANSI colors (bold and normal intensity), which terminal themes
// adapt for both light and dark backgrounds.
type Style struct {
	Error, Warning, Note, Help, Gutter, Bold, Dim, Reset string
	Keyword, String, Number, Comment, Label              string
	DiffAdd, DiffDel                                     string
}

// Colors is the color style; Plain has no escapes.
var (
	Colors = Style{
		Error: "\x1b[1;31m", Warning: "\x1b[1;33m", Note: "\x1b[1;36m", Help: "\x1b[1;32m",
		Gutter: "\x1b[1;34m", Bold: "\x1b[1m", Dim: "\x1b[2m", Reset: "\x1b[0m",
		Keyword: "\x1b[35m", String: "\x1b[32m", Number: "\x1b[36m", Comment: "\x1b[2m",
		Label: "\x1b[1;34m", DiffAdd: "\x1b[32m", DiffDel: "\x1b[31m",
	}
	Plain = Style{}
)

// Human renders diagnostics for a person.
type Human struct {
	W       io.Writer
	Style   Style
	Dir     string // paths are shown relative to Dir when below it
	Max     int    // at most this many errors are shown (0: 10)
	Sources *Sources
}

const tabWidth = 4

// Render writes the diagnostics, grouped by file in a stable order, then a
// summary line.
func (h *Human) Render(ds []Diagnostic) {
	if h.Sources == nil {
		h.Sources = NewSources()
	}
	max := h.Max
	if max == 0 {
		max = 10
	}
	shown := ds
	if len(shown) > max {
		shown = shown[:max]
	}
	for i := range shown {
		if i > 0 {
			fmt.Fprintln(h.W)
		}
		h.one(&shown[i])
	}
	errs, warns := Counts(ds)
	if errs+warns == 0 {
		return
	}
	files := map[string]bool{}
	var codes []string
	seenCode := map[string]bool{}
	for _, d := range ds {
		if d.Span.File != "" {
			files[d.Span.File] = true
		}
		if d.Code != CodeOther && !seenCode[d.Code] {
			seenCode[d.Code] = true
			codes = append(codes, d.Code)
		}
	}
	sort.Strings(codes)
	st := h.Style
	fmt.Fprintln(h.W)
	var parts []string
	if errs > 0 {
		parts = append(parts, plural(errs, "error"))
	}
	if warns > 0 {
		parts = append(parts, plural(warns, "warning"))
	}
	sum := strings.Join(parts, " and ")
	if len(files) > 1 {
		sum += fmt.Sprintf(" in %d files", len(files))
	}
	if len(ds) > len(shown) {
		sum += fmt.Sprintf(" (%d shown; --json lists all)", len(shown))
	}
	color := st.Error
	if errs == 0 {
		color = st.Warning
	}
	fmt.Fprintf(h.W, "%s%s%s\n", color, "error: build failed with "+sum, st.Reset)
	fixable := 0
	for _, d := range ds {
		if len(d.Fixes) > 0 && d.Fixes[0].Applicability == MachineApplicable {
			fixable++
		}
	}
	if fixable > 0 {
		fmt.Fprintf(h.W, "%s%s can be fixed automatically: run `marmot fix`%s\n", st.Dim, plural(fixable, "error"), st.Reset)
	}
	if len(codes) > 0 {
		fmt.Fprintf(h.W, "%sfor more about an error, run `marmot explain %s`%s\n", st.Dim, codes[0], st.Reset)
	}
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return strconv.Itoa(n) + " " + w + "s"
}

// RelPath shows p relative to dir when it is below dir.
func RelPath(dir, p string) string {
	if dir == "" || !filepath.IsAbs(p) {
		return p
	}
	if r, err := filepath.Rel(dir, p); err == nil && !strings.HasPrefix(r, "..") {
		if !strings.Contains(r, string(filepath.Separator)) {
			return "./" + r
		}
		return "./" + filepath.ToSlash(r)
	}
	return p
}

func (h *Human) sevColor(s Severity) string {
	switch s {
	case Warning:
		return h.Style.Warning
	case Note:
		return h.Style.Note
	}
	return h.Style.Error
}

func (h *Human) one(d *Diagnostic) {
	st := h.Style
	pad := strings.Repeat(" ", h.gutterWidth(d))
	gut := func() { fmt.Fprintf(h.W, "%s %s|%s\n", pad, st.Gutter, st.Reset) }
	// Header: severity[CODE]: message, then --> file:line:col (rustc anatomy;
	// plain mode keeps file:line:col: message for tools).
	code := ""
	if d.Code != "" && d.Code != CodeOther {
		code = "[" + d.Code + "]"
	}
	fmt.Fprintf(h.W, "%s%s%s%s: %s%s%s\n", h.sevColor(d.Severity), d.Severity, code, st.Reset, st.Bold, d.Message, st.Reset)
	f := (*srcFile)(nil)
	if d.Span.Valid() {
		fmt.Fprintf(h.W, "%s%s-->%s %s:%d:%d\n", pad, st.Gutter, st.Reset, RelPath(h.Dir, d.Span.File), d.Span.Line, max(d.Span.Col, 1))
		f = h.Sources.file(d.Span.File)
	}
	if f != nil {
		h.snippet(f, d)
	}
	if len(d.Notes)+len(d.Related)+len(d.Help)+len(d.Fixes) > 0 && f != nil {
		gut()
	}
	for _, n := range d.Notes {
		if strings.HasPrefix(n, "  ") {
			// Continuation of the note above (a list).
			fmt.Fprintf(h.W, "%s         %s\n", pad, n)
			continue
		}
		fmt.Fprintf(h.W, "%s %s=%s %snote:%s %s\n", pad, st.Gutter, st.Reset, st.Bold, st.Reset, n)
	}
	for _, r := range d.Related {
		fmt.Fprintf(h.W, "%s %s=%s %snote:%s %s at %s:%d\n", pad, st.Gutter, st.Reset, st.Bold, st.Reset, r.Message, RelPath(h.Dir, r.Span.File), r.Span.Line)
	}
	for _, x := range d.Help {
		fmt.Fprintf(h.W, "%s %s=%s %shelp:%s %s\n", pad, st.Gutter, st.Reset, st.Help, st.Reset, x)
	}
	if d.Bundle != "" {
		fmt.Fprintf(h.W, "%s %s=%s %snote:%s report bundle: %s\n", pad, st.Gutter, st.Reset, st.Bold, st.Reset, d.Bundle)
	}
	if d.FullMessage != "" {
		if p := writeFullMessage(d.FullMessage); p != "" {
			fmt.Fprintf(h.W, "%s %s=%s %snote:%s the full message, with the long type names, is in %s\n", pad, st.Gutter, st.Reset, st.Bold, st.Reset, p)
		}
	}
	for _, fx := range d.Fixes {
		h.fixDiff(fx, pad)
	}
}

func (h *Human) gutterWidth(d *Diagnostic) int {
	n := max(d.Span.EndLine, d.Span.Line)
	for _, l := range d.Labels {
		n = max(n, l.Span.EndLine)
	}
	for _, fx := range d.Fixes {
		for _, e := range fx.Edits {
			n = max(n, e.Span.EndLine, e.Span.Line)
		}
	}
	return max(len(strconv.Itoa(n)), 1)
}

// snippet draws the source lines of the span and labels with carets.
func (h *Human) snippet(f *srcFile, d *Diagnostic) {
	st := h.Style
	w := h.gutterWidth(d)
	type mark struct {
		sp      Span
		text    string
		primary bool
	}
	marks := []mark{{d.Span, d.Primary, true}}
	for _, l := range d.Labels {
		if l.Span.File == d.Span.File || l.Span.File == "" {
			marks = append(marks, mark{l.Span, l.Text, false})
		}
	}
	lines := map[int][]mark{}
	var order []int
	for _, m := range marks {
		end := max(m.sp.EndLine, m.sp.Line)
		if end-m.sp.Line > 3 {
			end = m.sp.Line // long spans: mark the first line only
		}
		for l := m.sp.Line; l <= end; l++ {
			if _, ok := lines[l]; !ok {
				order = append(order, l)
			}
			lines[l] = append(lines[l], m)
		}
	}
	sort.Ints(order)
	fmt.Fprintf(h.W, "%s %s|%s\n", strings.Repeat(" ", w), st.Gutter, st.Reset)
	prev := 0
	for _, ln := range order {
		text, ok := f.line(ln)
		if !ok {
			continue
		}
		if prev != 0 && ln > prev+1 {
			fmt.Fprintf(h.W, "%s%s...%s\n", strings.Repeat(" ", w-1), st.Gutter, st.Reset)
		}
		prev = ln
		fmt.Fprintf(h.W, "%s%*d |%s %s\n", st.Gutter, w, ln, st.Reset, h.highlight(text))
		// Caret rows: markers first, then the label texts right to left with
		// connectors, the way rustc draws several labels on one line.
		ms := lines[ln]
		var ds []drawn
		for _, m := range ms {
			sc, ec := 1, len(text)+1
			if ln == m.sp.Line {
				sc = m.sp.Col
			}
			last := max(m.sp.EndLine, m.sp.Line)
			if last-m.sp.Line > 3 {
				last = m.sp.Line
			}
			if ln == max(m.sp.EndLine, m.sp.Line) && m.sp.EndCol > 0 {
				ec = m.sp.EndCol
			}
			if ec <= sc {
				ec = sc + 1
			}
			x := drawn{vs: visualCol(text, sc), ve: visualCol(text, ec), ch: '-', col: st.Label}
			if m.primary {
				x.ch, x.col = '^', h.sevColor(d.Severity)
			}
			if ln == last {
				x.text = m.text
			}
			ds = append(ds, x)
		}
		width := 0
		for _, x := range ds {
			width = max(width, x.ve)
		}
		row := newRow(width)
		for _, x := range ds {
			if x.ch == '^' {
				row.fill(x.vs, x.ve, x.ch, x.col)
			}
		}
		for _, x := range ds {
			if x.ch != '^' {
				row.fill(x.vs, x.ve, x.ch, x.col)
			}
		}
		var texted []drawn
		for _, x := range ds {
			if x.text != "" {
				texted = append(texted, x)
			}
		}
		sort.SliceStable(texted, func(i, j int) bool { return texted[i].vs < texted[j].vs })
		out := row.render(st.Reset)
		if n := len(texted); n > 0 && texted[n-1].vs >= maxStart(ds, texted[n-1]) {
			out += " " + texted[n-1].col + texted[n-1].text + st.Reset
			texted = texted[:n-1]
		}
		if strings.TrimSpace(out) == "" && len(texted) == 0 {
			continue
		}
		gut := fmt.Sprintf("%s %s|%s ", strings.Repeat(" ", w), st.Gutter, st.Reset)
		fmt.Fprintln(h.W, gut+out)
		if len(texted) > 0 {
			conn := newRow(width)
			for _, x := range texted {
				conn.fill(x.vs, x.vs+1, '|', x.col)
			}
			fmt.Fprintln(h.W, gut+conn.render(st.Reset))
			for k := len(texted) - 1; k >= 0; k-- {
				r := newRow(width)
				for _, x := range texted[:k] {
					r.fill(x.vs, x.vs+1, '|', x.col)
				}
				fmt.Fprintln(h.W, gut+r.render(st.Reset)+strings.Repeat(" ", max(0, texted[k].vs-r.len()))+texted[k].col+texted[k].text+st.Reset)
			}
		}
	}
}

// writeFullMessage saves a long message to a file named by its hash in the
// temporary directory and returns the path.
func writeFullMessage(msg string) string {
	sum := sha256.Sum256([]byte(msg))
	p := filepath.Join(os.TempDir(), "marmot-msg-"+hex.EncodeToString(sum[:6])+".txt")
	if err := os.WriteFile(p, []byte(msg+"\n"), 0o644); err != nil {
		return ""
	}
	return p
}

// drawn is one mark on a caret row, in display columns.
type drawn struct {
	vs, ve int
	ch     rune
	col    string
	text   string
}

// maxStart is the largest start column among the marks other than x.
func maxStart(ds []drawn, x drawn) int {
	m := -1
	for _, y := range ds {
		if y != x {
			m = max(m, y.vs)
		}
	}
	return m
}

// row is one line of markers with a color per column.
type row struct {
	r []rune
	c []string
}

func newRow(n int) *row {
	r := &row{r: make([]rune, n), c: make([]string, n)}
	for i := range r.r {
		r.r[i] = ' '
	}
	return r
}

func (r *row) fill(a, b int, ch rune, col string) {
	for i := a; i < b; i++ {
		for i >= len(r.r) {
			r.r = append(r.r, ' ')
			r.c = append(r.c, "")
		}
		if r.r[i] == ' ' {
			r.r[i], r.c[i] = ch, col
		}
	}
}

// len is the width up to the last non-space column.
func (r *row) len() int {
	n := len(r.r)
	for n > 0 && r.r[n-1] == ' ' {
		n--
	}
	return n
}

func (r *row) render(reset string) string {
	var b strings.Builder
	cur := ""
	for i := 0; i < r.len(); i++ {
		if r.c[i] != cur {
			if cur != "" {
				b.WriteString(reset)
			}
			cur = r.c[i]
			b.WriteString(cur)
		}
		b.WriteRune(r.r[i])
	}
	if cur != "" {
		b.WriteString(reset)
	}
	return b.String()
}

// visualCol converts a 1-based byte column to a 0-based display column,
// expanding tabs.
func visualCol(line string, col int) int {
	v := 0
	for i, r := range line {
		if i >= col-1 {
			return v
		}
		if r == '\t' {
			v += tabWidth - v%tabWidth
		} else {
			v++
		}
	}
	return v + (col - 1 - len(line))
}

func visualWidth(line string) int { return visualCol(line, len(line)+1) }

func expandTabs(line string) string {
	if !strings.Contains(line, "\t") {
		return line
	}
	var b strings.Builder
	v := 0
	for _, r := range line {
		if r == '\t' {
			n := tabWidth - v%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			v += n
			continue
		}
		b.WriteRune(r)
		v++
	}
	return b.String()
}

// highlight colors Go tokens of one source line.
func (h *Human) highlight(line string) string {
	st := h.Style
	if st.Keyword == "" {
		return expandTabs(line)
	}
	src := []byte(line)
	fs := token.NewFileSet()
	tf := fs.AddFile("", -1, len(src))
	var sc scanner.Scanner
	sc.Init(tf, src, func(token.Position, string) {}, scanner.ScanComments)
	type span struct {
		a, b  int
		color string
	}
	var spans []span
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		off := tf.Offset(pos)
		n := len(lit)
		if lit == "" {
			n = len(tok.String())
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		var c string
		switch {
		case tok.IsKeyword():
			c = st.Keyword
		case tok == token.STRING || tok == token.CHAR:
			c = st.String
		case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
			c = st.Number
		case tok == token.COMMENT:
			c = st.Comment
		}
		if c != "" && off+n <= len(src) {
			spans = append(spans, span{off, off + n, c})
		}
	}
	var b strings.Builder
	last := 0
	v := 0
	write := func(s string) {
		for _, r := range s {
			if r == '\t' {
				n := tabWidth - v%tabWidth
				b.WriteString(strings.Repeat(" ", n))
				v += n
				continue
			}
			b.WriteRune(r)
			v++
		}
	}
	for _, s := range spans {
		if s.a < last {
			continue
		}
		write(line[last:s.a])
		b.WriteString(s.color)
		write(line[s.a:s.b])
		b.WriteString(st.Reset)
		last = s.b
	}
	write(line[last:])
	return b.String()
}

// fixDiff shows a fix as help with a diff of the lines it changes.
func (h *Human) fixDiff(fx Fix, pad string) {
	st := h.Style
	title := fx.Title
	if fx.Applicability == MaybeIncorrect {
		title += st.Dim + " (check it)" + st.Reset
	}
	fmt.Fprintf(h.W, "%shelp:%s %s\n", st.Help, st.Reset, title)
	w := len(pad)
	fmt.Fprintf(h.W, "%s %s|%s\n", pad, st.Gutter, st.Reset)
	for _, e := range fx.Edits {
		f := h.Sources.file(e.File)
		if f == nil {
			continue
		}
		before, after, first, ok := applyToLines(f, e)
		if !ok {
			continue
		}
		if len(fx.Edits) > 1 && e.File != fx.Edits[0].File {
			fmt.Fprintf(h.W, "%s%s-->%s %s\n", pad, st.Gutter, st.Reset, RelPath(h.Dir, e.File))
		}
		for i, l := range before {
			fmt.Fprintf(h.W, "%s%*d%s %s- %s%s\n", st.Gutter, w, first+i, st.Reset, st.DiffDel, expandTabs(l), st.Reset)
		}
		for i, l := range after {
			fmt.Fprintf(h.W, "%s%*d%s %s+ %s%s\n", st.Gutter, w, first+i, st.Reset, st.DiffAdd, expandTabs(l), st.Reset)
		}
	}
	fmt.Fprintf(h.W, "%s %s|%s\n", pad, st.Gutter, st.Reset)
}

// applyToLines returns the lines an edit touches, before and after.
func applyToLines(f *srcFile, e Edit) (before, after []string, first int, ok bool) {
	a := f.offset(e.Span.Line, e.Span.Col)
	b := f.offset(max(e.Span.EndLine, e.Span.Line), e.Span.EndCol)
	if e.Span.EndLine == 0 {
		b = a
	}
	if a < 0 || b < a || b > len(f.data) {
		return nil, nil, 0, false
	}
	first = e.Span.Line
	if e.NewText == "" && e.Span.Col == 1 && e.Span.EndCol == 1 && e.Span.EndLine > e.Span.Line {
		// Deletion of whole lines.
		for l := e.Span.Line; l < e.Span.EndLine; l++ {
			t, _ := f.line(l)
			before = append(before, t)
		}
		return before, nil, first, true
	}
	ls := f.lines[first-1]
	le := len(f.data)
	el, _ := f.pos(b)
	if b > a && b == f.lines[min(el-1, len(f.lines)-1)] && el > first {
		// The edit ends at a line start: it covers whole lines.
		el--
	}
	if el < len(f.lines) {
		le = f.lines[el] - 1
	}
	if le < b {
		le = b
	}
	old := string(f.data[ls:le])
	neu := string(f.data[ls:a]) + e.NewText + string(f.data[b:le])
	if a == b && e.Span.Col == 1 {
		// Pure insertion of whole lines.
		old = ""
		neu = strings.TrimSuffix(e.NewText, "\n")
		return nil, strings.Split(neu, "\n"), first, true
	}
	if old != "" {
		before = strings.Split(old, "\n")
	}
	if strings.TrimSpace(neu) != "" {
		after = strings.Split(neu, "\n")
	}
	return before, after, first, true
}
