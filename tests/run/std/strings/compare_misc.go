package main

import "strings"

func main() {
	println(strings.Compare("a", "b"), strings.Compare("b", "a"), strings.Compare("a", "a"), strings.Compare("", "a"))
	println(strings.HasPrefix("abc", ""), strings.HasPrefix("", "a"), strings.HasSuffix("abc", "bc"), strings.HasSuffix("c", "abc"))
	println(strings.Join(nil, ","), strings.Join([]string{"a"}, ","), strings.Join([]string{"a", "b", "c"}, ", "))
	before, after, ok := strings.Cut("a=b=c", "=")
	println(before, after, ok)
	before, after, ok = strings.Cut("abc", "=")
	println(before, after, ok)
	before, after, ok = strings.Cut("", "")
	println(before, after, ok)
	for _, s := range []string{"abc", "ABC"} {
		println(strings.ToUpper(s) == s)
	}
	println(strings.Clone("clone") == "clone")
	n := 0
	for range strings.SplitSeq("a,b,c", ",") {
		n++
	}
	println(n)
	for l := range strings.Lines("x\ny\nz") {
		print(len(l), " ")
	}
	println()
}
