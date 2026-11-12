// The terminal libraries cmd/marmot uses: lipgloss layout and x/ansi width
// and stripping, with no color profile (plain text output).
package main

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func main() {
	box := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1).Render("hello\nmarmot")
	fmt.Println(ansi.Strip(box))
	fmt.Println(lipgloss.Width(box), lipgloss.Height(box))
	row := lipgloss.JoinHorizontal(lipgloss.Top, "a\nb", " | ", "c")
	fmt.Println(ansi.Strip(row))
	colored := "\x1b[31mred\x1b[0m wide 世界"
	fmt.Println(ansi.StringWidth(colored), ansi.Strip(colored))
	fmt.Println(ansi.Truncate("abcdefgh", 5, "~"))
	fmt.Printf("%q\n", ansi.Wrap("one two three four", 9, ""))
}
