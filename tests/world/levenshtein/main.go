package main

import (
	"fmt"

	"github.com/agnivade/levenshtein"
)

func main() {
	pairs := [][2]string{
		{"", ""}, {"a", ""}, {"", "abc"}, {"kitten", "sitting"}, {"flaw", "lawn"}, {"saturday", "sunday"}, {"héllo", "hello"},
		{"日本語", "日本"}, {"abcdefghijklmnopqrstuvwxyz", "zyxwvutsrqponmlkjihgfedcba"}, {"same", "same"}, {"a😀b", "ab"},
	}
	for _, p := range pairs {
		d := levenshtein.ComputeDistance(p[0], p[1])
		fmt.Printf("%q %q %d\n", p[0], p[1], d)
	}
	long1, long2 := "", ""
	for i := 0; i < 300; i++ {
		long1 += string(rune('a' + i%26))
		long2 += string(rune('a' + (i*7)%26))
	}
	fmt.Println(levenshtein.ComputeDistance(long1, long2), levenshtein.ComputeDistance(long1, long1[:150]))
}
