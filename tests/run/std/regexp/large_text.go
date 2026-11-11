package main

import (
	"fmt"
	"regexp"
	"strings"
)

func main() {
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&sb, "user%d@host%d.example.com says hi; ", i, i%7)
	}
	text := sb.String()
	re := regexp.MustCompile(`(\w+)@(host\d)\.example\.com`)
	all := re.FindAllStringSubmatch(text, -1)
	fmt.Println(len(all), all[0][1], all[1999][2])
	counts := map[string]int{}
	for _, m := range all {
		counts[m[2]]++
	}
	fmt.Println(counts["host0"], counts["host6"])
	pathological := regexp.MustCompile(`(a+)+$`)
	fmt.Println(pathological.MatchString(strings.Repeat("a", 5000) + "b"))
	fmt.Println(len(re.ReplaceAllString(text, "$1")))
}
