package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
	"github.com/spf13/cobra"
)

// Marlo the sentinel, on a 16 by 16 grid: '#' is the body, 'w' the whistle, 'g' the ground.
// The body takes the terminal's own foreground colour, so it reads on light and dark themes;
// the whistle and the ground are blue.
var marlo = []string{
	".......##.......",
	"......####...w..",
	"......##.##...w.",
	"......######..w.",
	"......####...w..",
	".....######.....",
	".....#####......",
	".....#####......",
	".....#####......",
	".....#####......",
	"....#######.....",
	"................",
	"..gggggggggggg..",
	"................",
}

// artWidth is the narrowest terminal that shows the art next to the text.
const artWidth = 60

// printVersion writes the version in the output mode the other commands use. JSON mode (a
// coding agent, -json or MARMOT_OUTPUT=json) writes one object; plain mode, and human mode
// off a terminal, on a dumb one or a narrow one, the version line alone, as before. On a
// terminal in human mode Marlo stands to its left.
func printVersion(w io.Writer, mode diag.Mode, tty, color bool, width int, term string) {
	switch {
	case mode == diag.ModeJSON:
		b, _ := json.Marshal(struct {
			Schema  int    `json:"schema"`
			Kind    string `json:"kind"`
			Version string `json:"version"`
		}{diag.Schema, "version", version})
		fmt.Fprintln(w, string(b))
		return
	case mode != diag.ModeHuman || !tty || term == "dumb" || width < artWidth:
		fmt.Fprintln(w, version)
		return
	}
	text := []string{
		"",
		"",
		bold(version, color),
		dim("A Go compiler built on MLIR and LLVM", color),
		"",
		dim("marmot help shows the commands", color),
	}
	rows := halfBlocks(marlo, color)
	for i, r := range rows {
		line := "  " + r
		if i < len(text) && text[i] != "" {
			line += "   " + text[i]
		}
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
}

// halfBlocks draws two grid rows per line of text with the upper and lower half block.
func halfBlocks(grid []string, color bool) []string {
	var out []string
	for y := 0; y < len(grid); y += 2 {
		var b strings.Builder
		cur := ""
		set := func(s string) {
			if color && s != cur {
				b.WriteString(s)
				cur = s
			}
		}
		for x := 0; x < len(grid[y]); x++ {
			top := grid[y][x]
			bot := byte('.')
			if y+1 < len(grid) {
				bot = grid[y+1][x]
			}
			switch {
			case top == '.' && bot == '.':
				set("\x1b[0m")
				b.WriteByte(' ')
			case top == bot:
				set(fg(top))
				b.WriteString("█")
			case top == '.':
				set(fg(bot))
				b.WriteString("▄")
			case bot == '.':
				set(fg(top))
				b.WriteString("▀")
			default:
				set(fg(top) + bg(bot))
				b.WriteString("▀")
			}
		}
		if color && cur != "" && cur != "\x1b[0m" {
			b.WriteString("\x1b[0m")
		}
		out = append(out, b.String())
	}
	return out
}

func fg(c byte) string {
	if c == '#' {
		return "\x1b[0;39m" // the terminal's foreground
	}
	return "\x1b[0;34m"
}

func bg(c byte) string {
	if c == '#' {
		return "\x1b[49m"
	}
	return "\x1b[44m"
}

func bold(s string, color bool) string {
	if !color {
		return s
	}
	return "\x1b[1m" + s + "\x1b[0m"
}

func dim(s string, color bool) string {
	if !color {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
}

func cmdVersion(jsonFlag bool) {
	tty := diag.IsTerminal(os.Stdout)
	s := diag.Resolve(jsonFlag, "auto", os.Getenv, tty)
	printVersion(os.Stdout, s.Mode, tty, s.Color, diag.TermWidth(os.Stdout), os.Getenv("TERM"))
}

func versionCommand() *cobra.Command {
	var jsonFlag bool
	c := &cobra.Command{
		Use:   "version",
		Short: "Print the marmot version",
		Args:  cobra.NoArgs,
		Run:   func(*cobra.Command, []string) { cmdVersion(jsonFlag) },
	}
	c.Flags().BoolVar(&jsonFlag, "json", false, "machine readable output: one JSON object (also $MARMOT_OUTPUT=json, and the default under coding agents)")
	return c
}
