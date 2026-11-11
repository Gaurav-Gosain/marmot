package main

import (
	"io"
	"os"
)

func main() {
	f, _ := os.CreateTemp("", "marmot")
	defer os.Remove(f.Name())
	defer f.Close()
	f.Write([]byte("0123456789"))
	f.WriteAt([]byte("XY"), 3)
	ow := io.NewOffsetWriter(f, 8)
	ow.Write([]byte("ZZ"))
	f.Seek(0, io.SeekStart)
	b, _ := io.ReadAll(f)
	println(string(b))
	sr := io.NewSectionReader(f, 2, 4)
	s, _ := io.ReadAll(sr)
	println(string(s))
}
