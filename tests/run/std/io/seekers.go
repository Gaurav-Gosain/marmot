package main

import (
	"io"
	"strings"
)

func main() {
	r := strings.NewReader("0123456789")
	sr := io.NewSectionReader(r, 2, 5)
	b, _ := io.ReadAll(sr)
	println(string(b), sr.Size())
	sr.Seek(1, io.SeekStart)
	buf := make([]byte, 2)
	sr.Read(buf)
	println(string(buf))
	p, _ := r.Seek(-3, io.SeekEnd)
	println(p)
	p, err := r.Seek(-100, io.SeekStart)
	println(p, err != nil)
	println(io.SeekStart, io.SeekCurrent, io.SeekEnd)
}
