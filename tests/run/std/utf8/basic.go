package main

import "unicode/utf8"

func main() {
	s := "aé世😀"
	println(len(s), utf8.RuneCountInString(s), utf8.ValidString(s), utf8.ValidString("\xff"))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		println(i, r, size)
		i += size
	}
	r, size := utf8.DecodeRuneInString("\xff")
	println(r, size)
	r, size = utf8.DecodeRuneInString("")
	println(r, size)
	r, size = utf8.DecodeLastRuneInString(s)
	println(r, size)
	buf := make([]byte, 4)
	n := utf8.EncodeRune(buf, '世')
	println(n, buf[0], buf[1], buf[2])
	println(utf8.RuneLen('a'), utf8.RuneLen('é'), utf8.RuneLen('😀'), utf8.RuneLen(-1))
	println(string(utf8.AppendRune(nil, 'é')), utf8.FullRune([]byte{0xe4, 0xb8}), utf8.RuneError)
}
