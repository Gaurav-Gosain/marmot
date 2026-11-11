package main

import (
	"fmt"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

func main() {
	a := strings.SplitAfter("one\ntwo\nthree\nfour\nfive\nsix\nseven\n", "\n")
	b := strings.SplitAfter("one\n2\nthree\nfour\nfive\nsix\nseven\neight\n", "\n")
	ud := difflib.UnifiedDiff{A: a, B: b, FromFile: "a.txt", ToFile: "b.txt", Context: 2}
	s, err := difflib.GetUnifiedDiffString(ud)
	fmt.Println(err)
	fmt.Print(s)
	cd := difflib.ContextDiff{A: a, B: b, FromFile: "a", ToFile: "b", Context: 1}
	s, _ = difflib.GetContextDiffString(cd)
	fmt.Print(s)
	sm := difflib.NewMatcher(strings.Split("abcdefghij", ""), strings.Split("abxdefyhij", ""))
	fmt.Printf("%.4f %v\n", sm.Ratio(), sm.GetMatchingBlocks())
	for _, op := range sm.GetOpCodes() {
		fmt.Printf("%c %d %d %d %d\n", op.Tag, op.I1, op.I2, op.J1, op.J2)
	}
	for _, g := range sm.GetGroupedOpCodes(1) {
		fmt.Println(len(g))
	}
	fmt.Println(difflib.SplitLines("a\nb\nc"))
}
