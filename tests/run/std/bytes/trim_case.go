package main

import "bytes"

func main() {
	println(string(bytes.TrimSpace([]byte("  \t x \n"))), string(bytes.Trim([]byte("xxaxx"), "x")))
	println(string(bytes.TrimLeft([]byte("aabc"), "a")), string(bytes.TrimRight([]byte("abcc"), "c")))
	println(string(bytes.TrimPrefix([]byte("prefix"), []byte("pre"))), string(bytes.TrimSuffix([]byte("a.go"), []byte(".go"))))
	println(string(bytes.ToUpper([]byte("héllo"))), string(bytes.ToLower([]byte("HÉLLO"))), string(bytes.Title([]byte("ab cd"))))
	println(string(bytes.Repeat([]byte("ab"), 3)), len(bytes.Repeat(nil, 5)))
	println(string(bytes.Map(func(r rune) rune { return r + 1 }, []byte("abc"))))
	println(string(bytes.ReplaceAll([]byte("aaa"), []byte("a"), []byte("bb"))), string(bytes.Replace([]byte("aaa"), []byte("a"), []byte("b"), 2)))
	println(len(bytes.Runes([]byte("é世"))), string(bytes.ToValidUTF8([]byte("a\xffb"), []byte("?"))))
}
