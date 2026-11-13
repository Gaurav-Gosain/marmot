package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionPlain(t *testing.T) {
	for _, c := range []struct {
		name  string
		tty   bool
		width int
		term  string
	}{
		{"not a terminal", false, 100, "xterm-256color"},
		{"dumb terminal", true, 100, "dumb"},
		{"narrow terminal", true, artWidth - 1, "xterm-256color"},
	} {
		var b bytes.Buffer
		printVersion(&b, c.tty, true, c.width, c.term)
		if got := b.String(); got != version+"\n" {
			t.Errorf("%s: got %q, want the version line alone", c.name, got)
		}
	}
}

func TestVersionArt(t *testing.T) {
	var b bytes.Buffer
	printVersion(&b, true, false, 80, "xterm-256color")
	out := b.String()
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NO_COLOR output has escape codes:\n%s", out)
	}
	if !strings.Contains(out, version) || !strings.Contains(out, "▀") {
		t.Errorf("art or version missing:\n%s", out)
	}
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if n := len([]rune(ln)); n > artWidth {
			t.Errorf("line of %d columns is wider than %d: %q", n, artWidth, ln)
		}
	}
	if lines := strings.Count(out, "\n"); lines != (len(marlo)+1)/2 {
		t.Errorf("%d lines, want %d", lines, (len(marlo)+1)/2)
	}

	b.Reset()
	printVersion(&b, true, true, 80, "xterm-256color")
	if !strings.Contains(b.String(), "\x1b[0;39m") {
		t.Errorf("colour output does not use the terminal's foreground for the body")
	}
}
