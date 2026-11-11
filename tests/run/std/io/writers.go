package main

import (
	"bytes"
	"io"
)

func main() {
	var a, b bytes.Buffer
	w := io.MultiWriter(&a, &b)
	io.WriteString(w, "hello")
	w.Write([]byte(" world"))
	println(a.String(), b.String() == a.String())
	n, err := io.WriteString(io.Discard, "dropped")
	println(n, err == nil)
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("through pipe"))
		pw.Close()
	}()
	data, _ := io.ReadAll(pr)
	println(string(data))
	pr2, pw2 := io.Pipe()
	pw2.CloseWithError(io.ErrClosedPipe)
	_, err = pr2.Read(make([]byte, 1))
	println(err == io.ErrClosedPipe)
}
