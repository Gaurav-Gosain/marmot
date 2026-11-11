package main

import "unicode/utf8"

func main() {
	var b []byte
	for _, r := range []rune{'a', 'é', '世', '😀', 0xd800, -5} {
		b = utf8.AppendRune(b, r)
	}
	println(len(b), utf8.Valid(b), utf8.ValidString(string(b)))
	println(utf8.Valid([]byte{0xff}), utf8.Valid(nil), utf8.Valid([]byte("ok")))
	println(utf8.FullRune([]byte{0xe4, 0xb8}), utf8.FullRune([]byte{0xe4, 0xb8, 0x96}), utf8.FullRune([]byte{0xff}), utf8.FullRune(nil))
}
