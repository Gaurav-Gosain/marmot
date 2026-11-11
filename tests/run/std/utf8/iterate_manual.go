package main

import "unicode/utf8"

func main() {
	s := "aé世😀\xffz"
	i := 0
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		print(r, ":", w, " ")
		i += w
	}
	println()
	for i := len(s); i > 0; {
		r, w := utf8.DecodeLastRuneInString(s[:i])
		print(r, " ")
		i -= w
	}
	println()
	n := 0
	for range s {
		n++
	}
	println(n)
}
