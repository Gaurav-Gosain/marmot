package main

import "unicode"

func main() {
	for _, r := range []rune{'a', 'Z', '5', ' ', '\n', 'é', '世', '_', '!', 0x1F600, 'ǅ', '٣'} {
		println(r, unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsSpace(r), unicode.IsUpper(r), unicode.IsLower(r), unicode.IsPunct(r), unicode.IsPrint(r), unicode.IsNumber(r), unicode.IsControl(r))
	}
	println(string(unicode.ToUpper('é')), string(unicode.ToLower('Σ')), string(unicode.ToTitle('ǆ')), unicode.ToUpper('1'))
	println(unicode.Is(unicode.Han, '世'), unicode.Is(unicode.Latin, 'a'), unicode.In('α', unicode.Greek, unicode.Latin))
	println(unicode.MaxRune, unicode.ReplacementChar, unicode.SimpleFold('A'), unicode.SimpleFold('a'))
}
