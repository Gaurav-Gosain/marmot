package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Gaurav-Gosain/marmot/internal/diag"
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

// printVersion writes the version. Off a terminal, on a dumb one or a narrow one it is the
// version line alone, as before; on a terminal Marlo stands to its left.
func printVersion(w io.Writer, tty, color bool, width int, term string) {
	if !tty || term == "dumb" || width < artWidth {
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

func cmdVersion() {
	tty := diag.IsTerminal(os.Stdout)
	color := tty && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	printVersion(os.Stdout, tty, color, diag.TermWidth(os.Stdout), os.Getenv("TERM"))
}
