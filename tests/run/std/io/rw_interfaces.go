package main

import (
	"bytes"
	"io"
	"os"
)

func main() {
	var r io.Reader = bytes.NewReader([]byte("abc"))
	_, isSeeker := r.(io.Seeker)
	_, isWT := r.(io.WriterTo)
	_, isCloser := r.(io.Closer)
	println(isSeeker, isWT, isCloser)
	var w io.Writer = os.Stdout
	_, isSW := w.(io.StringWriter)
	_, isRF := w.(io.ReaderFrom)
	println(isSW, isRF)
	var rc io.ReadCloser = io.NopCloser(r)
	println(rc.Close() == nil)
	var buf bytes.Buffer
	var _ io.ByteScanner = &buf
	var _ io.RuneScanner = &buf
	println("ok")
}
