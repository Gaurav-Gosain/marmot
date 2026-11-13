package main

import (
	"fmt"
	"os"
	"path/filepath"

	"marmotworld/bind_zlib/zlib"
)

func main() {
	fmt.Println(zlib.LibVersion() != "", zlib.Crc32(0, []byte("hello")))
	path := filepath.Join(os.TempDir(), "marmot-bind-test.gz")
	f := zlib.Open(path, "wb")
	if f == nil {
		fmt.Println("open failed")
		return
	}
	data := []byte("the quick brown fox jumps over the lazy dog\n")
	fmt.Println(f.Write(data))
	if err := f.Close(); err != nil {
		fmt.Println(err)
	}
	r := zlib.Open(path, "rb")
	buf := make([]byte, 100)
	n := r.Read(buf)
	fmt.Printf("%d %q\n", n, buf[:n])
	fmt.Println(r.Close())
	os.Remove(path)
	var s zlib.Stream
	fmt.Println(zlib.InflateEnd(&s))
}
