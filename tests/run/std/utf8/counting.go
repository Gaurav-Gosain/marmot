package main

import "unicode/utf8"

func main() {
	for _, s := range []string{"", "abc", "héllo", "世界", "😀😀", "a\xffb", "\xe4\xb8"} {
		println(len(s), utf8.RuneCountInString(s), utf8.RuneCount([]byte(s)), len([]rune(s)))
	}
	println(utf8.UTFMax, utf8.MaxRune, utf8.RuneError, utf8.RuneSelf)
	println(utf8.RuneStart(0x80), utf8.RuneStart('a'), utf8.RuneStart(0xc3))
}
