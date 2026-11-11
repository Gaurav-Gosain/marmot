package main

import "bytes"

func main() {
	var b bytes.Buffer
	for i := 0; i < 100000; i++ {
		b.WriteByte(byte('a' + i%26))
	}
	println(b.Len(), b.Cap() >= b.Len())
	s := b.Next(26)
	println(string(s), b.Len())
	b.Truncate(10)
	println(b.Len(), string(b.Bytes()[:3]))
	var c bytes.Buffer
	c.Write(bytes.Repeat([]byte("xy"), 40000))
	n, _ := c.WriteTo(&b)
	println(n, b.Len(), c.Len())
	defer func() { println(recover() != nil) }()
	b.Truncate(1 << 30)
}
