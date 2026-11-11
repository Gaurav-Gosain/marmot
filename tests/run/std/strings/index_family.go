package main

import "strings"

func main() {
	s := "go gopher, go go"
	println(strings.Index(s, "go"), strings.Index(s, ""), strings.Index(s, "zz"), strings.Index("", "a"), strings.Index("", ""))
	println(strings.LastIndex(s, "go"), strings.LastIndex(s, ""), strings.LastIndex(s, "x"))
	println(strings.IndexAny(s, "pr"), strings.IndexAny(s, ""), strings.LastIndexAny(s, "go"))
	println(strings.IndexByte(s, 'h'), strings.LastIndexByte(s, 'o'), strings.IndexRune(s, 'r'), strings.IndexRune("é世", '世'))
	println(strings.IndexFunc(s, func(r rune) bool { return r == ',' }), strings.LastIndexFunc(s, func(r rune) bool { return r == 'g' }))
	println(strings.Contains("", ""), strings.Contains("a", ""), strings.ContainsAny("abc", "xyzc"), strings.ContainsFunc("abc", func(r rune) bool { return r > 'b' }))
	println(strings.Count("cheese", "e"), strings.Count("five", ""), strings.Count("aaaa", "aa"))
	long := strings.Repeat("ab", 500) + "needle" + strings.Repeat("ab", 500)
	println(strings.Index(long, "needle"), strings.Index(long, "needles"), strings.LastIndex(long, "ab"))
}
