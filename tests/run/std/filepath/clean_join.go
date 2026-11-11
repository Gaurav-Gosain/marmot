package main

import "path/filepath"

func main() {
	for _, p := range []string{"", ".", "/", "a//b", "a/./b", "a/b/../c", "../a", "/../a", "a/b/", "/a/b/..", "a/../..", "//x///y"} {
		println(filepath.Clean(p))
	}
	println(filepath.Join("a", "b", "../c"), filepath.Join("/", "x"), filepath.Join(), filepath.Join("", ""), filepath.Join("a/", "/b"))
	println(filepath.Base("/a/b.txt"), filepath.Base("a/"), filepath.Base(""), filepath.Base("/"), filepath.Dir("/a/b/c"), filepath.Dir("a"), filepath.Dir("/"), filepath.Dir(""))
}
