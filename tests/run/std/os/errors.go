package main

import (
	"errors"
	"io/fs"
	"os"
)

func main() {
	_, err := os.Open("/nonexistent/file.txt")
	println(err != nil, os.IsNotExist(err), errors.Is(err, fs.ErrNotExist), errors.Is(err, os.ErrNotExist))
	var pe *fs.PathError
	println(errors.As(err, &pe), pe.Op, pe.Path)
	_, err = os.ReadFile("/nonexistent")
	println(err.Error())
	err = os.Mkdir("/nonexistent/a/b", 0o755)
	println(err != nil, os.IsNotExist(err))
	d, _ := os.MkdirTemp("", "marmot")
	defer os.RemoveAll(d)
	err = os.Mkdir(d, 0o755)
	println(os.IsExist(err), errors.Is(err, fs.ErrExist))
	_, err = os.ReadDir(d + "/missing")
	println(err != nil)
	err = os.Remove(d + "/missing")
	println(os.IsNotExist(err))
}
