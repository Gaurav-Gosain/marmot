package main

import (
	"fmt"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func main() {
	dmp := diffmatchpatch.New()
	a := "The quick brown fox jumps over the lazy dog."
	b := "That quick brown fox jumped over a lazy dog!"
	diffs := dmp.DiffMain(a, b, true)
	diffs = dmp.DiffCleanupSemantic(diffs)
	for _, d := range diffs {
		fmt.Printf("%d %q\n", d.Type, d.Text)
	}
	fmt.Println(dmp.DiffPrettyText(diffs))
	fmt.Println(dmp.DiffToDelta(diffs), dmp.DiffLevenshtein(diffs))
	patches := dmp.PatchMake(a, diffs)
	text, ok := dmp.PatchApply(patches, a)
	fmt.Println(text, ok)
	fmt.Println(dmp.PatchToText(patches))
	fmt.Println(dmp.MatchMain("abcdefghijk", "fgh", 5))
	fmt.Println(dmp.DiffHalfMatch("1234567890", "a345678z"))
	fmt.Println(dmp.DiffPrettyHtml(dmp.DiffMain("a<b", "a>b&c", false)))
	lines, lines2, arr := dmp.DiffLinesToChars("a\nb\nc\n", "a\nx\nc\n")
	d := dmp.DiffMain(lines, lines2, false)
	fmt.Println(dmp.DiffCharsToLines(d, arr))
}
