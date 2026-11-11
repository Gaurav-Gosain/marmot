package main

import "unicode/utf8"

func main() {
	for _, r := range []rune{0, 'a', 0x7f, 0x80, 0x7ff, 0x800, 0xd7ff, 0xe000, 0xffff, 0x10000, 0x10ffff, 0xd800, 0x110000, -1} {
		buf := make([]byte, 4)
		n := utf8.EncodeRune(buf, r)
		back, size := utf8.DecodeRune(buf[:n])
		println(r, n, utf8.RuneLen(r), back, size, utf8.ValidRune(r))
	}
}
