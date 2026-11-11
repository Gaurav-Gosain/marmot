package main

import (
	"bufio"
	"strings"
)

func main() {
	long := strings.Repeat("x", 70000)
	sc := bufio.NewScanner(strings.NewReader(long + "\nshort\n"))
	println(sc.Scan(), sc.Err() == bufio.ErrTooLong)
	sc = bufio.NewScanner(strings.NewReader(long + "\nshort\n"))
	sc.Buffer(make([]byte, 0, 1024), 1<<20)
	for sc.Scan() {
		println(len(sc.Text()))
	}
	println(sc.Err() == nil)
}
