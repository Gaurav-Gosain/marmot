package main

import "os"

func main() {
	wd, err := os.Getwd()
	println(len(wd) > 0, err == nil)
	d, _ := os.MkdirTemp("", "marmot")
	defer os.RemoveAll(d)
	println(os.Chdir(d) == nil)
	wd2, _ := os.Getwd()
	println(len(wd2) > 0)
	os.WriteFile("rel.txt", []byte("r"), 0o644)
	b, err := os.ReadFile("rel.txt")
	println(string(b), err == nil)
	os.Chdir(wd)
	println(len(os.TempDir()) > 0)
	h, err := os.Hostname()
	println(len(h) > 0, err == nil)
}
