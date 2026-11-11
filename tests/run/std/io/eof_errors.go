package main

import (
	"errors"
	"io"
	"strings"
)

func main() {
	println(io.EOF.Error(), io.ErrUnexpectedEOF.Error(), io.ErrShortWrite.Error(), io.ErrNoProgress.Error())
	r := strings.NewReader("")
	n, err := r.Read(make([]byte, 1))
	println(n, err == io.EOF, errors.Is(err, io.EOF))
	n, err = r.Read(nil)
	println(n, err == io.EOF)
	b, err := io.ReadAll(strings.NewReader(""))
	println(len(b), err == nil)
	_, err = io.ReadFull(strings.NewReader("x"), make([]byte, 3))
	println(err == io.ErrUnexpectedEOF)
	println(io.ErrClosedPipe.Error(), io.ErrShortBuffer.Error())
}
