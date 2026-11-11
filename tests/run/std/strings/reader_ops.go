package main

import (
	"io"
	"strings"
)

func main() {
	r := strings.NewReader("héllo wörld")
	println(r.Len(), r.Size())
	c, size, _ := r.ReadRune()
	println(c, size)
	c, size, _ = r.ReadRune()
	println(c, size)
	r.UnreadRune()
	b, _ := r.ReadByte()
	println(b)
	r.Seek(0, io.SeekStart)
	buf := make([]byte, 4)
	n, _ := r.Read(buf)
	println(n, string(buf[:n]))
	n2, _ := r.ReadAt(buf, 7)
	println(n2, string(buf[:n2]))
	r.Seek(-2, io.SeekEnd)
	rest, _ := io.ReadAll(r)
	println(len(rest))
	_, err := r.ReadByte()
	println(err == io.EOF)
	var sb strings.Builder
	r.Seek(0, io.SeekStart)
	m, _ := r.WriteTo(&sb)
	println(m, sb.String())
}
