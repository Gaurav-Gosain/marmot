package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func main() {
	s := "Hello, Wörld! 123 世界"
	letters, digits, spaces, upper, other := 0, 0, 0, 0, 0
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		case unicode.IsDigit(r):
			digits++
		case unicode.IsSpace(r):
			spaces++
		default:
			other++
		}
	}
	println(letters, digits, spaces, upper, other, utf8.RuneCountInString(s))
	println(strings.ToUpper(s), strings.Map(unicode.ToLower, s))
	println(strings.IndexFunc(s, unicode.IsDigit), strings.TrimFunc("  x1 ", unicode.IsSpace))
}
