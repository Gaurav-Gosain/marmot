package main

import (
	"fmt"
	"regexp"
)

func main() {
	tests := []struct{ pat, s string }{
		{`^abc$`, "abc"}, {`a.c`, "a\nc"}, {`(?s)a.c`, "a\nc"}, {`(?i)hello`, "HeLLo"},
		{`(?m)^b$`, "a\nb\nc"}, {`\bword\b`, "a word here"}, {`[^aeiou\s]+`, "hello world"},
		{`a{2,3}`, "aaaa"}, {`a+?`, "aaa"}, {`(a|ab)(c|bcd)`, "abcd"}, {`\p{L}+`, "héllo wörld"},
		{`[[:alpha:]]+`, "abc123"}, {`\d+(\.\d+)?`, "pi 3.14159"}, {`^$`, ""}, {`(?U)a+`, "aaa"},
		{`[\p{Han}]+`, "x世界y"}, {`\x41\u0042`, "AB"}, {`.`, "世"}, {`a|b|`, "c"},
	}
	for _, t := range tests {
		re := regexp.MustCompile(t.pat)
		fmt.Printf("%-18s %q %q\n", t.pat, t.s, re.FindString(t.s))
	}
	re := regexp.MustCompile(`(a)(b)?`)
	fmt.Println(re.FindStringSubmatchIndex("a"), re.FindStringSubmatch("a"))
	fmt.Println(regexp.MustCompilePOSIX(`a+|a+b`).FindString("aab"))
}
