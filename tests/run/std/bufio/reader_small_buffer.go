package main

import (
	"bufio"
	"strings"
)

func main() {
	r := bufio.NewReaderSize(strings.NewReader("abcdefghijklmnopqrstuvwxyz\nshort\n"), 16)
	println(r.Size())
	l, pre, _ := r.ReadLine()
	println(string(l), pre)
	l, pre, _ = r.ReadLine()
	println(string(l), pre)
	l, pre, _ = r.ReadLine()
	println(string(l), pre)
	_, err := r.Peek(100)
	println(err == bufio.ErrBufferFull || err != nil)
	s, err := bufio.NewReaderSize(strings.NewReader("aaaaaaaaaaaaaaaaaaaaaaaa"), 16).ReadSlice('\n')
	println(len(s), err == bufio.ErrBufferFull)
}
