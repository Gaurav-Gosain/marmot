package main

import (
	"io"
	"strings"
)

func main() {
	var b strings.Builder
	for i := 0; i < 5; i++ {
		b.WriteString("ab")
		b.WriteByte('-')
		b.WriteRune('é')
	}
	println(b.Len(), b.String())
	b.Reset()
	println(b.Len())
	r := strings.NewReader("hello world")
	buf := make([]byte, 5)
	n, _ := r.Read(buf)
	println(n, string(buf))
	rest, _ := io.ReadAll(r)
	println(string(rest), r.Len())
	_, err := r.Read(buf)
	println(err == io.EOF)
	rp := strings.NewReplacer("a", "1", "b", "2")
	println(rp.Replace("abcab"))
}
