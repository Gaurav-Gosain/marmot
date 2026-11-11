package main

import "strings"

func main() {
	s := "Hello, World! Hello!"
	println(strings.Contains(s, "World"), strings.ContainsRune(s, 'W'), strings.ContainsAny(s, "xyz!"))
	println(strings.Index(s, "Hello"), strings.LastIndex(s, "Hello"), strings.IndexByte(s, 'W'), strings.Index(s, "zzz"))
	println(strings.HasPrefix(s, "Hell"), strings.HasSuffix(s, "!"), strings.Count(s, "l"), strings.Count("abc", ""))
	println(strings.ToUpper(s), strings.ToLower(s))
	println(strings.Repeat("ab", 3), strings.Replace(s, "Hello", "Bye", 1), strings.ReplaceAll(s, "l", "L"))
	println(strings.TrimSpace("  \t x y \n"), strings.Trim("xxhixx", "x"), strings.TrimLeft("xxhix", "x"), strings.TrimPrefix("prefix-a", "prefix-"), strings.TrimSuffix("a.go", ".go"))
	println(strings.EqualFold("Go", "GO"), strings.Compare("a", "b"), strings.Title("x"))
	parts := strings.Split("a,b,c,,d", ",")
	println(len(parts), parts[2], parts[3] == "")
	println(len(strings.Fields("  a  b\tc\n")), strings.Join(parts, "|"))
	println(len(strings.SplitN("a,b,c,d", ",", 2)), strings.SplitN("a,b,c,d", ",", 2)[1])
	before, after, found := strings.Cut("key=value", "=")
	println(before, after, found)
	println(strings.Map(func(r rune) rune { return r + 1 }, "abc"))
	println(strings.IndexFunc("abc1", func(r rune) bool { return r >= '0' && r <= '9' }))
}
