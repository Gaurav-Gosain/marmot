package main

import (
	"bufio"
	"strings"
)

func main() {
	r := bufio.NewReader(strings.NewReader("hello world\nsecond line\nrest"))
	l, err := r.ReadString('\n')
	println(len(l), err == nil)
	b, _ := r.ReadBytes(' ')
	println(string(b))
	c, _ := r.ReadByte()
	println(c)
	r.UnreadByte()
	ru, sz, _ := r.ReadRune()
	println(ru, sz)
	pk, _ := r.Peek(3)
	println(string(pk), r.Buffered() > 0)
	line, isPrefix, _ := r.ReadLine()
	println(string(line), isPrefix)
	rest, err := r.ReadString('\n')
	println(rest, err != nil)
	_, err = r.ReadByte()
	println(err != nil)
	d, _ := bufio.NewReader(strings.NewReader("abcdef")).Discard(4)
	println(d)
}
