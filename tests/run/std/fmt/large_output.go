package main

import (
	"fmt"
	"strings"
)

func main() {
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&sb, "%d:%x;", i, i*31)
	}
	s := sb.String()
	println(len(s), s[:20], s[len(s)-10:])
	big := fmt.Sprintf("%s|%10000s|", "a", "b")
	println(len(big))
	fmt.Println(strings.Repeat("=", 40))
	for i := 0; i < 5; i++ {
		fmt.Printf("%-3d|%6.2f|%s\n", i, float64(i)*1.1, strings.Repeat("*", i))
	}
	table := [][]string{{"name", "qty"}, {"apple", "3"}, {"kiwi", "12"}}
	for _, r := range table {
		fmt.Printf("%-8s%5s\n", r[0], r[1])
	}
}
