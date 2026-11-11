package main

import (
	"os"
	"sort"
)

func main() {
	dir, _ := os.MkdirTemp("", "marmot")
	defer os.RemoveAll(dir)
	println(os.Mkdir(dir+"/sub", 0o755) == nil, os.Mkdir(dir+"/sub", 0o755) != nil)
	println(os.MkdirAll(dir+"/x/y/z", 0o755) == nil)
	os.WriteFile(dir+"/b.txt", nil, 0o600)
	os.WriteFile(dir+"/a.txt", []byte("12"), 0o600)
	es, _ := os.ReadDir(dir)
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
		print(e.IsDir(), " ")
	}
	println()
	println(sort.StringsAreSorted(names), len(names), names[0], names[1])
	println(os.Rename(dir+"/a.txt", dir+"/c.txt") == nil)
	_, err := os.Stat(dir + "/a.txt")
	println(os.IsNotExist(err))
	println(os.Remove(dir+"/c.txt") == nil, os.Remove(dir+"/c.txt") != nil, os.Remove(dir+"/x") != nil)
	println(os.RemoveAll(dir+"/x") == nil)
	st, _ := os.Stat(dir + "/x")
	println(st == nil)
}
