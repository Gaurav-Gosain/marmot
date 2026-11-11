package main

import (
	"bytes"
	"io"
)

func main() {
	r := bytes.NewReader([]byte("0123456789"))
	println(r.Len(), r.Size())
	p := make([]byte, 3)
	r.Read(p)
	println(string(p), r.Len())
	r.Seek(2, io.SeekCurrent)
	r.Read(p)
	println(string(p))
	n, err := r.ReadAt(p, 8)
	println(n, err == io.EOF)
	r.Reset([]byte("new"))
	all, _ := io.ReadAll(r)
	println(string(all))
	b, err := r.ReadByte()
	println(b, err == io.EOF)
}
