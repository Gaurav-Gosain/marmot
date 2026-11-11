package main

import "unicode/utf8"

func main() {
	for _, s := range []string{"\xff", "\xc0\x80", "\xe0\x80\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xc3", "\xe4\xb8", "\xf0\x9f\x98", "a\x80", "\x80\x80", "\xf8\x88\x80\x80\x80"} {
		r, size := utf8.DecodeRuneInString(s)
		println(r, size, utf8.ValidString(s), utf8.RuneCountInString(s), utf8.FullRuneInString(s))
	}
}
