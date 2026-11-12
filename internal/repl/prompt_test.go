package repl

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Without a terminal the prompt reads plain lines, joining lines while the
// entry is incomplete.
func TestPlainInput(t *testing.T) {
	r, w, _ := os.Pipe()
	w.WriteString("a\nb {\nc\n}\n")
	w.Close()
	p := New(Options{In: r, Complete: func(s string) bool { return strings.Count(s, "{") == strings.Count(s, "}") }})
	var got []string
	for {
		l, err := p.ReadLine()
		if errors.Is(err, ErrEOF) {
			break
		}
		got = append(got, l)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b {\nc\n}" {
		t.Errorf("got %q", got)
	}
}

func TestHistoryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h")
	p := New(Options{HistoryPath: path, In: os.Stdin})
	p.addHistory("x := 1")
	p.addHistory("for {\n}")
	p.addHistory("for {\n}")
	p.Close()
	q := New(Options{HistoryPath: path, In: os.Stdin})
	if len(q.history) != 2 || q.history[1] != "for {\n}" {
		t.Errorf("history %q", q.history)
	}
}
