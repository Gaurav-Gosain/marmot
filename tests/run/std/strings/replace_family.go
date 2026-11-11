package main

import "strings"

func main() {
	s := "oink oink oink"
	println(strings.Replace(s, "k", "ky", 2), strings.Replace(s, "oink", "moo", -1), strings.Replace(s, "", "-", 3))
	println(strings.ReplaceAll("abc", "", "_"), strings.ReplaceAll("", "", "x"), strings.ReplaceAll("aaa", "aa", "b"))
	r := strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;")
	println(r.Replace("a<b>&c"))
	r2 := strings.NewReplacer("a", "1", "aa", "2", "aaa", "3")
	println(r2.Replace("aaaa"))
	r3 := strings.NewReplacer("", "X")
	println(r3.Replace("ab"))
	r4 := strings.NewReplacer("hello", "bye", "world", "all")
	println(r4.Replace("hello world, hello!"))
	println(strings.Repeat("-", 0) == "", strings.Repeat("ab", 3), len(strings.Repeat("x", 100000)))
}
