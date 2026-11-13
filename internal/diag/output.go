package diag

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Mode is an output mode.
type Mode int

const (
	// ModePlain is the text the go tool prints: file:line:col: message, one
	// per line. It is the default when stderr is not a terminal, and what
	// existing tests and tools parse.
	ModePlain Mode = iota
	// ModeHuman adds source snippets, carets, suggestions and color.
	ModeHuman
	// ModeJSON writes JSON Lines for programs and coding agents.
	ModeJSON
)

// AgentEnv lists environment variables set by coding agents. When one is
// set (non-empty and not "0"), marmot defaults to JSON output.
var AgentEnv = []string{
	"CLAUDECODE",     // Claude Code
	"AI_AGENT",       // generic convention
	"CODEX_SANDBOX",  // OpenAI Codex CLI
	"GEMINI_CLI",     // Gemini CLI
	"CURSOR_AGENT",   // Cursor agent
	"OPENCODE",       // opencode
	"GOOSE_TERMINAL", // goose
}

// Env abstracts the process environment for tests.
type Env func(string) string

// Settings is the resolved output configuration.
type Settings struct {
	Mode  Mode
	Color bool
	Agent string // the agent variable that selected JSON, if any
}

// Resolve picks the output mode. Precedence: the --json flag, then
// MARMOT_OUTPUT (json, human, plain), then an agent environment, then human
// when stderr is a terminal, else plain. color is "auto", "always" or
// "never"; NO_COLOR turns auto off.
func Resolve(jsonFlag bool, color string, getenv Env, tty bool) Settings {
	if getenv == nil {
		getenv = os.Getenv
	}
	s := Settings{Mode: ModePlain}
	switch {
	case jsonFlag:
		s.Mode = ModeJSON
	case getenv("MARMOT_OUTPUT") != "":
		switch strings.ToLower(getenv("MARMOT_OUTPUT")) {
		case "json":
			s.Mode = ModeJSON
		case "human":
			s.Mode = ModeHuman
		default:
			s.Mode = ModePlain
		}
	default:
		for _, v := range AgentEnv {
			if x := getenv(v); x != "" && x != "0" {
				s.Mode, s.Agent = ModeJSON, v
				break
			}
		}
		if s.Mode == ModePlain && tty {
			s.Mode = ModeHuman
		}
	}
	if s.Mode == ModeHuman {
		switch color {
		case "always":
			s.Color = true
		case "never":
			s.Color = false
		default:
			s.Color = tty && getenv("NO_COLOR") == "" && getenv("TERM") != "dumb"
		}
	}
	return s
}

// IsTerminal reports whether f is a character device.
func IsTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// Result is the last JSON line of a command.
type Result struct {
	Kind       string `json:"kind"` // "result"
	Schema     int    `json:"schema"`
	Command    string `json:"command,omitempty"`
	OK         bool   `json:"ok"`
	Errors     int    `json:"errors"`
	Warnings   int    `json:"warnings"`
	Output     string `json:"output,omitempty"`
	DurationMs int64  `json:"durationMs"`
	ExitCode   int    `json:"exitCode,omitempty"`
}

// jsonDiag is the wire form: the diagnostic plus the schema version.
type jsonDiag struct {
	Schema int `json:"schema"`
	*Diagnostic
	Explain string `json:"explain,omitempty"`
}

// WriteJSON writes one JSON object per diagnostic.
func WriteJSON(w io.Writer, ds []Diagnostic) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for i := range ds {
		d := &ds[i]
		if d.Kind == "" {
			d.Kind = "diagnostic"
		}
		jd := jsonDiag{Schema: Schema, Diagnostic: d}
		if d.Code != "" && d.Code != CodeOther {
			jd.Explain = "marmot explain " + d.Code
		}
		if err := enc.Encode(jd); err != nil {
			return err
		}
	}
	return nil
}

// WriteResult writes the final result line.
func WriteResult(w io.Writer, r Result) error {
	r.Kind, r.Schema = "result", Schema
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

// WritePlain writes diagnostics as file:line:col: message lines, the form
// the go tool uses, with detail lines indented by a tab.
func WritePlain(w io.Writer, ds []Diagnostic, dir string) {
	for _, d := range ds {
		var b strings.Builder
		if d.Span.Valid() {
			b.WriteString(RelPath(dir, d.Span.File))
			b.WriteString(":")
			b.WriteString(itoa(d.Span.Line))
			if d.Span.Col > 0 {
				b.WriteString(":")
				b.WriteString(itoa(d.Span.Col))
			}
			b.WriteString(": ")
		}
		b.WriteString(d.Message)
		b.WriteString("\n")
		for _, n := range d.Notes {
			b.WriteString("\t")
			b.WriteString(n)
			b.WriteString("\n")
		}
		fmt.Fprint(w, b.String())
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
