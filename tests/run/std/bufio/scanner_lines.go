package main

import (
	"bufio"
	"strings"
)

func main() {
	in := "first\nsecond\r\n\nlast no newline"
	sc := bufio.NewScanner(strings.NewReader(in))
	for sc.Scan() {
		println(len(sc.Text()), sc.Text())
	}
	println(sc.Err() == nil)
	sc = bufio.NewScanner(strings.NewReader(""))
	println(sc.Scan())
	sc = bufio.NewScanner(strings.NewReader("\n\n"))
	n := 0
	for sc.Scan() {
		n++
	}
	println(n)
	sc = bufio.NewScanner(strings.NewReader("a\nb\n"))
	for sc.Scan() {
		b := sc.Bytes()
		println(len(b), b[0])
	}
}
