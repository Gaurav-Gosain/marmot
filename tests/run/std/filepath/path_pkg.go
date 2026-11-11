package main

import "path"

func main() {
	println(path.Join("a", "b", "..", "c"), path.Clean("/a/../b//c/."), path.Base("/x/y.z"), path.Dir("/x/y.z"), path.Ext("f.tar.gz"), path.IsAbs("/q"))
	d, f := path.Split("a/b/c")
	println(d, f)
	ok, _ := path.Match("a/*/c", "a/b/c")
	println(ok)
}
