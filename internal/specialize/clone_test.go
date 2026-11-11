package specialize

import "testing"

const cloneProgram = `package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
)

type rec struct{ k int }

func isOdd(x int) bool { return x%2 == 1 }

func main() {
	xs := []int{1, 3, 5, 7, 9, 11, 40, 41}
	calls := 0
	for _, t := range []int{-1, 0, 1, 6, 7, 12, 41, 99} {
		i := sort.Search(len(xs), func(i int) bool { calls++; return xs[i] >= t })
		fmt.Println(t, i, calls)
	}
	fmt.Println(sort.Search(0, func(int) bool { panic("never") }))
	recs := []rec{{4}, {8}, {15}, {16}}
	fmt.Println(slices.IndexFunc(recs, func(r rec) bool { return r.k > 10 }), slices.IndexFunc(xs, isOdd), slices.IndexFunc([]string{}, func(s string) bool { return s == "" }))
	fmt.Println(slices.ContainsFunc(xs, func(x int) bool { return x > 40 }), slices.ContainsFunc(xs, isOdd))
	drop := 'b'
	m := strings.Map(func(r rune) rune {
		if r == drop {
			return -1
		}
		return unicode.ToUpper(r)
	}, "abcabc éß \xff日本")
	fmt.Printf("%q\n", m)
	fmt.Printf("%q\n", strings.Map(unicode.ToLower, "MiXeD Ü"))
	same := "unchanged"
	fmt.Println(strings.Map(func(r rune) rune { return r }, same) == same)
	fmt.Println(strings.IndexFunc("hello, wörld", unicode.IsPunct), strings.IndexFunc("abc", func(r rune) bool { return r > 'z' }))
	fmt.Println(strings.ContainsFunc("abc1", unicode.IsDigit))
	var f func(int) bool
	defer func() { fmt.Println("recovered", recover() != nil) }()
	fmt.Println(sort.Search(3, f))
}
`

func TestCloneDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds programs with the go tool")
	}
	diffTest(t, cloneProgram, 8)
}
