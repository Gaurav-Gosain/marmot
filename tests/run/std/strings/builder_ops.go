package main

import "strings"

func main() {
	var b strings.Builder
	println(b.Len(), b.Cap() >= 0, b.String() == "")
	b.Grow(100)
	println(b.Cap() >= 100)
	for i := 0; i < 10; i++ {
		b.WriteString("x")
		b.WriteByte('y')
		b.WriteRune('世')
		n, _ := b.Write([]byte{'z'})
		_ = n
	}
	println(b.Len(), b.String()[:12])
	s := b.String()
	b.WriteString("more")
	println(len(s), b.Len())
	b.Reset()
	b.WriteString("fresh")
	println(b.String(), s[:2])
	var big strings.Builder
	for i := 0; i < 10000; i++ {
		big.WriteString("0123456789")
	}
	println(big.Len())
}
