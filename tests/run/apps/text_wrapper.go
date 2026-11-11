// Text formatting: greedy and optimal (minimum raggedness) word wrapping, justification,
// centering, tables with unicode-aware widths, and a simple markdown-ish reflow.
package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const lorem = "The quick brown fox jumps over the lazy dog while the five boxing wizards jump quickly. " +
	"Pack my box with five dozen liquor jugs; sphinx of black quartz, judge my vow. " +
	"How vexingly quick daft zebras jump! Supercalifragilisticexpialidocious words must be broken somewhere."

func words(s string) []string { return strings.Fields(s) }

func width(s string) int { return utf8.RuneCountInString(s) }

func greedy(text string, w int) []string {
	var lines []string
	var cur []string
	n := 0
	for _, word := range words(text) {
		for width(word) > w {
			if len(cur) > 0 {
				lines = append(lines, strings.Join(cur, " "))
				cur, n = nil, 0
			}
			r := []rune(word)
			k, dash := w-1, "-"
			if w == 1 {
				k, dash = 1, ""
			}
			lines = append(lines, string(r[:k])+dash)
			word = string(r[k:])
		}
		add := width(word)
		if len(cur) > 0 {
			add++
		}
		if n+add > w && len(cur) > 0 {
			lines = append(lines, strings.Join(cur, " "))
			cur, n = nil, 0
			add = width(word)
		}
		cur = append(cur, word)
		n += add
	}
	if len(cur) > 0 {
		lines = append(lines, strings.Join(cur, " "))
	}
	return lines
}

// optimal minimizes the sum of squared trailing spaces (except the last line) using dynamic
// programming over word break positions.
func optimal(text string, w int) []string {
	ws := words(text)
	n := len(ws)
	const inf = 1 << 60
	cost := make([]int, n+1)
	brk := make([]int, n+1)
	for i := n - 1; i >= 0; i-- {
		cost[i] = inf
		l := -1
		for j := i; j < n; j++ {
			l += width(ws[j]) + 1
			if l > w && j > i {
				break
			}
			var c int
			if j == n-1 {
				c = 0
			} else {
				slack := w - l
				c = slack*slack + cost[j+1]
			}
			if l > w {
				c += 1000000
			}
			if c < cost[i] {
				cost[i] = c
				brk[i] = j + 1
			}
		}
	}
	var lines []string
	for i := 0; i < n; i = brk[i] {
		lines = append(lines, strings.Join(ws[i:brk[i]], " "))
	}
	return lines
}

func justify(line string, w int, last bool) string {
	ws := words(line)
	if last || len(ws) == 1 {
		return line
	}
	total := 0
	for _, x := range ws {
		total += width(x)
	}
	gaps := len(ws) - 1
	spaces := w - total
	var sb strings.Builder
	for i, x := range ws {
		sb.WriteString(x)
		if i < gaps {
			n := spaces / gaps
			if i < spaces%gaps {
				n++
			}
			sb.WriteString(strings.Repeat(" ", n))
		}
	}
	return sb.String()
}

func center(s string, w int) string {
	pad := w - width(s)
	if pad <= 0 {
		return s
	}
	l := pad / 2
	return strings.Repeat(" ", l) + s + strings.Repeat(" ", pad-l)
}

func box(lines []string) string {
	w := 0
	for _, l := range lines {
		w = max(w, width(l))
	}
	var sb strings.Builder
	sb.WriteString("+" + strings.Repeat("-", w+2) + "+\n")
	for _, l := range lines {
		sb.WriteString("| " + l + strings.Repeat(" ", w-width(l)) + " |\n")
	}
	sb.WriteString("+" + strings.Repeat("-", w+2) + "+\n")
	return sb.String()
}

func table(rows [][]string) string {
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	ws := make([]int, cols)
	for _, r := range rows {
		for i, c := range r {
			ws[i] = max(ws[i], width(c))
		}
	}
	var sb strings.Builder
	for ri, r := range rows {
		for i := 0; i < cols; i++ {
			c := ""
			if i < len(r) {
				c = r[i]
			}
			if i > 0 {
				sb.WriteString(" | ")
			}
			sb.WriteString(c + strings.Repeat(" ", ws[i]-width(c)))
		}
		sb.WriteString("\n")
		if ri == 0 {
			for i := 0; i < cols; i++ {
				if i > 0 {
					sb.WriteString("-+-")
				}
				sb.WriteString(strings.Repeat("-", ws[i]))
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func raggedness(lines []string, w int) int {
	t := 0
	for i, l := range lines {
		if i < len(lines)-1 {
			s := w - width(l)
			t += s * s
		}
	}
	return t
}

func main() {
	for _, w := range []int{20, 34, 60} {
		g := greedy(lorem, w)
		o := optimal(lorem, w)
		fmt.Printf("width %d: greedy %d lines (rag %d), optimal %d lines (rag %d)\n", w, len(g), raggedness(g, w), len(o), raggedness(o, w))
	}
	fmt.Println(box(greedy(lorem, 40)))
	o := optimal(lorem, 40)
	fmt.Println(box(o))
	var j []string
	for i, l := range o {
		j = append(j, justify(l, 40, i == len(o)-1))
	}
	fmt.Println(box(j))
	fmt.Println(box([]string{center("Title", 20), center("世界の言葉", 20), center("toolongtitlethatoverflows", 20)}))
	fmt.Print(table([][]string{
		{"name", "qty", "note"},
		{"apple", "3", "red"},
		{"kiwi", "12", "green and fuzzy"},
		{"日本語", "7"},
		{"é", "1000000", "x"},
	}))
	for _, w := range []int{1, 2, 5} {
		fmt.Printf("%d: %q\n", w, greedy("a bb ccc dddd eeeee ffffff", w))
	}
	fmt.Printf("%q %q\n", greedy("", 10), optimal("", 10))
	long := strings.Repeat("lorem ipsum dolor sit amet ", 200)
	g := greedy(long, 72)
	o2 := optimal(long, 72)
	fmt.Println(len(g), len(o2), raggedness(o2, 72) <= raggedness(g, 72))
	fmt.Println(strings.Count(strings.Join(g, " "), "lorem"), strings.Count(strings.Join(o2, " "), "lorem"))
}
