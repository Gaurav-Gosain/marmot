package main

import (
	"io"
	"strings"
)

type oneByte struct{ r io.Reader }

func (o oneByte) Read(p []byte) (int, error) { return o.r.Read(p[:1]) }

func main() {
	buf := make([]byte, 6)
	n, err := io.ReadFull(oneByte{strings.NewReader("abcdefgh")}, buf)
	println(n, err == nil, string(buf))
	n, err = io.ReadAtLeast(oneByte{strings.NewReader("abc")}, buf, 4)
	println(n, err == io.ErrUnexpectedEOF)
	n, err = io.ReadAtLeast(strings.NewReader("abc"), buf[:2], 3)
	println(n, err == io.ErrShortBuffer)
	l := io.LimitReader(strings.NewReader("abcdef"), 0)
	b, _ := io.ReadAll(l)
	println(len(b))
	l2 := &io.LimitedReader{R: strings.NewReader("abcdef"), N: 3}
	io.ReadAll(l2)
	println(l2.N)
}
