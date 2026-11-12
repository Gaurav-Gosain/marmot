package main

import "strings"

type Logger struct{ lines []string }

func (l *Logger) Write(p []byte) (int, error) {
	l.lines = append(l.lines, strings.TrimSpace(string(p)))
	return len(p), nil
}

func (l *Logger) Count() int { return len(l.lines) }
