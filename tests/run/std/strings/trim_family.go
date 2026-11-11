package main

import "strings"

func main() {
	println("[" + strings.TrimSpace(" \t\n x y \r\n") + "]")
	println("["+strings.Trim("xxabcxx", "x")+"]", "["+strings.Trim("abc", "")+"]", "["+strings.Trim("", "x")+"]")
	println(strings.TrimLeft("aabbcc", "ab"), strings.TrimRight("aabbcc", "bc"))
	println(strings.TrimPrefix("prefix", "pre"), strings.TrimPrefix("prefix", "x"), strings.TrimSuffix("file.go", ".go"), strings.TrimSuffix("file", ".go"))
	println(strings.TrimFunc("123abc456", func(r rune) bool { return r >= '0' && r <= '9' }))
	println(strings.TrimLeftFunc("  x ", func(r rune) bool { return r == ' ' }) + "|")
	println(strings.Trim("éaé", "é"), strings.Trim("世界世", "世"))
	a, ok := strings.CutPrefix("foobar", "foo")
	b, ok2 := strings.CutSuffix("foobar", "baz")
	println(a, ok, b, ok2)
}
