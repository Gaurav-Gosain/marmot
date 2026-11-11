package main

import "path/filepath"

func main() {
	for _, c := range [][2]string{{"*.go", "main.go"}, {"*.go", "dir/main.go"}, {"a?c", "abc"}, {"[a-c]x", "bx"}, {"[^a-c]x", "bx"}, {"a*b*c", "aXXbYYc"}, {`\*`, "*"}, {"[", "a"}, {"", ""}} {
		ok, err := filepath.Match(c[0], c[1])
		println(c[0], c[1], ok, err != nil)
	}
	for _, c := range [][2]string{{"/a/b", "/a/b/c/d"}, {"/a/b", "/a/c"}, {"a", "a"}, {"/a", "b"}, {"a/b", "../c"}} {
		r, err := filepath.Rel(c[0], c[1])
		println(r, err != nil)
	}
}
