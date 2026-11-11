package main

import (
	"bufio"
	"errors"
)

type failW struct{ n int }

func (f *failW) Write(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, errors.New("disk full")
	}
	f.n -= len(p)
	return len(p), nil
}

func main() {
	w := bufio.NewWriterSize(&failW{n: 10}, 8)
	_, err := w.WriteString("12345678")
	println(err == nil)
	_, err = w.WriteString("abcdefgh")
	println(err == nil)
	_, err = w.WriteString("ABCDEFGH")
	println(err != nil)
	println(w.Flush() != nil)
	_, err = w.WriteString("x")
	println(err.Error())
}
