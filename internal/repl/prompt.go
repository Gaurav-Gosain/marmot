// Package repl is a small line editor for terminal REPLs: ghost text
// completion, syntax highlighting, persistent history, multi-line input,
// and a plain fallback when input is not a terminal (pipes, scripts).
//
//	p := repl.New(repl.Options{Prompt: "> ", HistoryPath: path, Suggest: fn})
//	defer p.Close()
//	for {
//		line, err := p.ReadLine()
//		if errors.Is(err, repl.ErrEOF) { break }
//		if errors.Is(err, repl.ErrCanceled) { continue }
//		handle(line)
//	}
package repl

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"
)

var (
	// ErrEOF is returned by ReadLine at the end of input (ctrl-d on an
	// empty line).
	ErrEOF = errors.New("repl: end of input")
	// ErrCanceled is returned when the line was abandoned with ctrl-c.
	ErrCanceled = errors.New("repl: canceled")
)

// Options configure a Prompt.
type Options struct {
	Prompt      string // first line prompt
	Continue    string // prompt of continuation lines (default "... ")
	HistoryPath string // file the history is kept in; "" for none
	// Suggest returns the text to show after the cursor as ghost text for
	// the input so far (only the part not yet typed), or "".
	Suggest func(input string) string
	// Highlight colors one line of input; nil for none.
	Highlight func(line string) string
	// Complete reports whether the input is a complete entry; when it is
	// not, Enter starts a continuation line. nil: every line is complete.
	Complete func(input string) bool
	In       *os.File  // default os.Stdin
	Out      io.Writer // default os.Stdout
}

// Prompt reads lines.
type Prompt struct {
	opt     Options
	tty     bool
	history []string
	reader  *bufio.Reader
}

// New makes a Prompt. Without a terminal it reads plain lines.
func New(opt Options) *Prompt {
	if opt.In == nil {
		opt.In = os.Stdin
	}
	if opt.Out == nil {
		opt.Out = os.Stdout
	}
	if opt.Continue == "" {
		opt.Continue = "... "
	}
	p := &Prompt{opt: opt, tty: term.IsTerminal(opt.In.Fd()), reader: bufio.NewReader(opt.In)}
	if opt.HistoryPath != "" {
		if data, err := os.ReadFile(opt.HistoryPath); err == nil {
			for l := range strings.SplitSeq(string(data), "\n") {
				if l != "" {
					p.history = append(p.history, strings.ReplaceAll(l, "\\n", "\n"))
				}
			}
		}
	}
	return p
}

// Interactive reports whether the prompt edits a terminal line.
func (p *Prompt) Interactive() bool { return p.tty }

// Close saves the history.
func (p *Prompt) Close() error {
	if p.opt.HistoryPath == "" {
		return nil
	}
	h := p.history
	if len(h) > 1000 {
		h = h[len(h)-1000:]
	}
	var b strings.Builder
	for _, l := range h {
		b.WriteString(strings.ReplaceAll(l, "\n", "\\n"))
		b.WriteByte('\n')
	}
	os.MkdirAll(filepath.Dir(p.opt.HistoryPath), 0o755)
	return os.WriteFile(p.opt.HistoryPath, []byte(b.String()), 0o600)
}

func (p *Prompt) addHistory(s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	if n := len(p.history); n > 0 && p.history[n-1] == s {
		return
	}
	p.history = append(p.history, s)
}

// ReadLine reads one entry (possibly several lines when Complete says the
// input continues).
func (p *Prompt) ReadLine() (string, error) {
	if !p.tty {
		return p.readPlain()
	}
	old, err := term.MakeRaw(p.opt.In.Fd())
	if err != nil {
		return p.readPlain()
	}
	defer term.Restore(p.opt.In.Fd(), old)
	var lines []string
	for {
		prompt := p.opt.Prompt
		if len(lines) > 0 {
			prompt = p.opt.Continue
		}
		line, err := p.edit(prompt, lines)
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
		all := strings.Join(lines, "\n")
		if p.opt.Complete == nil || p.opt.Complete(all) || strings.TrimSpace(line) == "" && len(lines) > 1 {
			p.addHistory(all)
			return all, nil
		}
	}
}

func (p *Prompt) readPlain() (string, error) {
	var lines []string
	for {
		l, err := p.reader.ReadString('\n')
		if err != nil && l == "" {
			if len(lines) > 0 {
				return strings.Join(lines, "\n"), nil
			}
			return "", ErrEOF
		}
		lines = append(lines, strings.TrimRight(l, "\r\n"))
		all := strings.Join(lines, "\n")
		if p.opt.Complete == nil || p.opt.Complete(all) {
			return all, nil
		}
	}
}

// edit runs the line editor for one physical line.
func (p *Prompt) edit(prompt string, before []string) (string, error) {
	w := p.opt.Out
	buf := []rune{}
	pos := 0
	hist := len(p.history)
	saved := ""
	ghost := ""
	draw := func() {
		ghost = ""
		if pos == len(buf) && p.opt.Suggest != nil && len(buf) > 0 {
			ghost = p.opt.Suggest(strings.Join(append(append([]string{}, before...), string(buf)), "\n"))
		}
		text := string(buf)
		if p.opt.Highlight != nil {
			text = p.opt.Highlight(text)
		}
		var b strings.Builder
		b.WriteString("\r\x1b[K")
		b.WriteString(prompt)
		b.WriteString(text)
		if ghost != "" {
			b.WriteString("\x1b[2m" + ghost + "\x1b[0m")
		}
		// Move the cursor back from the end of what was drawn.
		back := len(buf) - pos + utf8.RuneCountInString(ghost)
		if back > 0 {
			b.WriteString("\x1b[" + itoa(back) + "D")
		}
		io.WriteString(w, b.String())
	}
	draw()
	in := p.reader
	for {
		r, _, err := in.ReadRune()
		if err != nil {
			return "", ErrEOF
		}
		switch r {
		case '\r', '\n':
			ghost = ""
			io.WriteString(w, "\r\n")
			return string(buf), nil
		case 3: // ctrl-c
			io.WriteString(w, "^C\r\n")
			return "", ErrCanceled
		case 4: // ctrl-d
			if len(buf) == 0 && len(before) == 0 {
				io.WriteString(w, "\r\n")
				return "", ErrEOF
			}
			if pos < len(buf) {
				buf = append(buf[:pos], buf[pos+1:]...)
			}
		case 1: // ctrl-a
			pos = 0
		case 5: // ctrl-e
			if pos == len(buf) && ghost != "" {
				buf = append(buf, []rune(ghost)...)
			}
			pos = len(buf)
		case 9: // tab: accept the ghost text
			if ghost != "" {
				buf = append(buf[:pos], append([]rune(ghost), buf[pos:]...)...)
				pos += utf8.RuneCountInString(ghost)
			} else {
				buf = append(buf[:pos], append([]rune("\t"), buf[pos:]...)...)
				pos++
			}
		case 127, 8: // backspace
			if pos > 0 {
				buf = append(buf[:pos-1], buf[pos:]...)
				pos--
			}
		case 21: // ctrl-u
			buf, pos = buf[pos:], 0
		case 11: // ctrl-k
			buf = buf[:pos]
		case 23: // ctrl-w: delete the word before the cursor
			i := pos
			for i > 0 && buf[i-1] == ' ' {
				i--
			}
			for i > 0 && buf[i-1] != ' ' {
				i--
			}
			buf, pos = append(buf[:i], buf[pos:]...), i
		case 12: // ctrl-l
			io.WriteString(w, "\x1b[H\x1b[2J")
		case 27: // escape sequences
			seq := readEscape(in)
			switch seq {
			case "[D":
				if pos > 0 {
					pos--
				}
			case "[C":
				if pos < len(buf) {
					pos++
				} else if ghost != "" {
					buf = append(buf, []rune(ghost)...)
					pos = len(buf)
				}
			case "[H", "[1~", "OH":
				pos = 0
			case "[F", "[4~", "OF":
				pos = len(buf)
			case "[3~":
				if pos < len(buf) {
					buf = append(buf[:pos], buf[pos+1:]...)
				}
			case "[A":
				if hist > 0 {
					if hist == len(p.history) {
						saved = string(buf)
					}
					hist--
					buf = []rune(strings.ReplaceAll(p.history[hist], "\n", "; "))
					pos = len(buf)
				}
			case "[B":
				if hist < len(p.history) {
					hist++
					if hist == len(p.history) {
						buf = []rune(saved)
					} else {
						buf = []rune(strings.ReplaceAll(p.history[hist], "\n", "; "))
					}
					pos = len(buf)
				}
			}
		default:
			if r >= ' ' {
				buf = append(buf[:pos], append([]rune{r}, buf[pos:]...)...)
				pos++
			}
		}
		draw()
	}
}

// readEscape reads the rest of an escape sequence after ESC.
func readEscape(in *bufio.Reader) string {
	var b strings.Builder
	r, _, err := in.ReadRune()
	if err != nil {
		return ""
	}
	b.WriteRune(r)
	if r != '[' && r != 'O' {
		return b.String()
	}
	for {
		r, _, err := in.ReadRune()
		if err != nil {
			break
		}
		b.WriteRune(r)
		if r >= '@' && r <= '~' && r != '[' {
			break
		}
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d [20]byte
	i := len(d)
	for n > 0 {
		i--
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d[i:])
}
