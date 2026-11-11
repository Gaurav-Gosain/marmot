package main

import (
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	d, _ := os.MkdirTemp("", "marmot")
	defer os.RemoveAll(d)
	os.MkdirAll(filepath.Join(d, "a", "b"), 0o755)
	os.MkdirAll(filepath.Join(d, "c"), 0o755)
	for _, f := range []string{"a/1.txt", "a/b/2.go", "c/3.txt", "top.txt"} {
		os.WriteFile(filepath.Join(d, f), []byte("x"), 0o644)
	}
	filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
		r, _ := filepath.Rel(d, p)
		println(r, e.IsDir())
		return nil
	})
	m, _ := filepath.Glob(filepath.Join(d, "*", "*.txt"))
	println(len(m))
	m, _ = filepath.Glob(filepath.Join(d, "*.txt"))
	println(len(m), filepath.Base(m[0]))
	filepath.Walk(d, func(p string, i os.FileInfo, err error) error {
		if i.IsDir() && i.Name() == "b" {
			return filepath.SkipDir
		}
		if !i.IsDir() {
			print(i.Name(), " ")
		}
		return nil
	})
	println()
}
