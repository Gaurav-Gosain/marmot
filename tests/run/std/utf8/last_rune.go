package main

import "unicode/utf8"

func main() {
	for _, s := range []string{"", "a", "é", "世", "a😀", "a\xff", "ab\xe4\xb8", "\x80"} {
		r, size := utf8.DecodeLastRuneInString(s)
		b, bs := utf8.DecodeLastRune([]byte(s))
		println(r, size, b, bs)
	}
}
