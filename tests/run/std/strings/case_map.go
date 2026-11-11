package main

import "strings"

func main() {
	println(strings.ToUpper("hello World 123"), strings.ToLower("HELLO World 123"))
	println(strings.ToUpper("héllo ñ ß"), strings.ToLower("ÉCOLE Σ"))
	println(strings.ToTitle("abc"), strings.EqualFold("Straße", "STRASSE"), strings.EqualFold("Σ", "ς"), strings.EqualFold("go", "GO"))
	println(strings.Map(func(r rune) rune {
		if r == 'a' {
			return -1
		}
		return r + 1
	}, "banana"))
	println(strings.Map(func(r rune) rune { return 'x' }, "日本"))
	println(strings.ToValidUTF8("a\xffb\xfe\xfdc", "?"))
	println(strings.Title("hello wor-ld"))
}
