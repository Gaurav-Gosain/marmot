package main

import "strconv"

func main() {
	for _, s := range []string{"", "plain", "tab\there", "nl\n", "quote\"", "back\\", "uni é 世", "\x00\x01\x7f", "\xff", "emoji 😀"} {
		q := strconv.Quote(s)
		println(q, strconv.QuoteToASCII(s))
		u, err := strconv.Unquote(q)
		println(u == s, err == nil)
	}
	println(strconv.QuoteRune('x'), strconv.QuoteRune('\n'), strconv.QuoteRune('世'), strconv.QuoteRuneToASCII('世'))
	for _, s := range []string{`"a"`, "`raw\\n`", `'c'`, `"bad`, `abc`, `"é"`, `"\x41"`} {
		u, err := strconv.Unquote(s)
		println(u, err != nil)
	}
}
