package main

import (
	"fmt"
	"os"
	"testing/fstest"

	"github.com/bmatcuk/doublestar/v4"
)

func main() {
	cases := [][2]string{
		{"*.go", "main.go"}, {"*.go", "dir/main.go"}, {"**/*.go", "a/b/c.go"}, {"**/*.go", "c.go"}, {"a/**/b", "a/b"}, {"a/**/b", "a/x/y/b"},
		{"{a,b}/*.txt", "b/x.txt"}, {"[a-c]?[!d]", "bxe"}, {"[a-c]?[!d]", "bxd"}, {"a/*/c", "a/b/c"}, {"a/*/c", "a/b/d/c"}, {`\*`, "*"}, {"**", ""}, {"", ""},
		{"file[0-9].{txt,log}", "file7.log"}, {"日本*", "日本語"}, {"a?c", "a/c"}, {"{a,b{c,d}}x", "bdx"},
	}
	for _, c := range cases {
		ok, err := doublestar.Match(c[0], c[1])
		fmt.Printf("%-22q %-14q %v %v\n", c[0], c[1], ok, err)
	}
	_, err := doublestar.Match("[", "a")
	fmt.Println(err, doublestar.ErrBadPattern == err)
	fmt.Println(doublestar.ValidatePattern("a/{b,c}/d"), doublestar.ValidatePattern("a/{b,c/d"))
	fsys := fstest.MapFS{
		"a/x.go": {}, "a/b/y.go": {}, "a/b/c/z.go": {}, "a/b/c/w.txt": {}, "top.go": {}, "d/.hidden/q.go": {},
	}
	for _, pat := range []string{"**/*.go", "a/**", "a/*/*.go", "{a,d}/**/*.go", "*.txt", "a/b/c/*"} {
		m, err := doublestar.Glob(fsys, pat)
		fmt.Println(pat, m, err)
	}
	n := 0
	doublestar.GlobWalk(fsys, "**/*.go", func(p string, d os.DirEntry) error { n++; return nil })
	fmt.Println(n)
	pm, _ := doublestar.PathMatch("a/*", "a/b")
	base, rest := doublestar.SplitPattern("a/b/*/c/**/d")
	fmt.Println(pm, base, rest)
}
