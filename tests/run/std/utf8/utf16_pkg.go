package main

import "unicode/utf16"

func main() {
	rs := []rune{'a', '世', '😀', 0x10ffff}
	u := utf16.Encode(rs)
	println(len(u), u[0], u[1], u[2], u[3])
	back := utf16.Decode(u)
	println(len(back), back[2] == '😀', back[3] == 0x10ffff)
	println(utf16.IsSurrogate(0xd800), utf16.IsSurrogate('a'))
	r1, r2 := utf16.EncodeRune('😀')
	println(r1, r2, utf16.DecodeRune(r1, r2))
	println(string(utf16.Decode([]uint16{0xd800, 'a'})) == "�a")
}
