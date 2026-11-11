package main

import "path/filepath"

func main() {
	for _, p := range []string{"file.txt", "a/b.tar.gz", ".hidden", "noext", "dir.d/file", "a."} {
		println(filepath.Ext(p))
	}
	d, f := filepath.Split("/a/b/c.txt")
	println(d, f)
	d, f = filepath.Split("noslash")
	println(d == "", f)
	println(filepath.IsAbs("/a"), filepath.IsAbs("a"), filepath.VolumeName("/a") == "")
	println(string(filepath.Separator), string(filepath.ListSeparator), filepath.ToSlash("a/b"), filepath.FromSlash("a/b"))
	for _, p := range filepath.SplitList("/a:/b::/c") {
		print("[", p, "]")
	}
	println()
}
