package main

import "bytes"

func main() {
	a := []byte("hello")
	b := bytes.Clone(a)
	b[0] = 'J'
	println(string(a), string(b))
	t := bytes.TrimSpace([]byte("  x "))
	t[0] = 'y'
	println(string(t))
	var buf bytes.Buffer
	buf.WriteString("abc")
	view := buf.Bytes()
	view[0] = 'X'
	println(buf.String())
	buf.WriteString("def")
	println(buf.String(), len(view))
	s := bytes.Fields([]byte("a b"))
	s[0][0] = 'Z'
	println(string(s[0]), string(s[1]))
	println(bytes.MinRead)
}
