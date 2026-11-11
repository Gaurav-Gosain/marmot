package main

import (
	"bytes"
	"io"
)

func main() {
	var b bytes.Buffer
	println(b.Len(), b.String() == "", b.Cap() >= 0)
	b.WriteString("hello ")
	b.WriteByte('w')
	b.WriteRune('ö')
	b.Write([]byte("rld"))
	println(b.Len(), b.String())
	p := make([]byte, 5)
	n, _ := b.Read(p)
	println(n, string(p), b.String())
	c, _ := b.ReadByte()
	println(c)
	b.UnreadByte()
	r, size, _ := b.ReadRune()
	println(r, size)
	rest := b.Next(2)
	println(string(rest), b.Len())
	b.Truncate(1)
	println(b.String())
	b.Reset()
	_, err := b.ReadByte()
	println(err == io.EOF)
	b.Grow(64)
	println(b.Available() >= 64)
}
