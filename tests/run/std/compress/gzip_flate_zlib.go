package main

import (
	"bytes"
	"compress/bzip2"
	"compress/flate"
	"compress/gzip"
	"compress/lzw"
	"compress/zlib"
	"fmt"
	"io"
	"strings"
)

func main() {
	data := []byte(strings.Repeat("marmot compression test data; ", 2000) + "tail")
	for _, lvl := range []int{gzip.NoCompression, gzip.BestSpeed, gzip.DefaultCompression, gzip.BestCompression, gzip.HuffmanOnly} {
		var buf bytes.Buffer
		w, _ := gzip.NewWriterLevel(&buf, lvl)
		w.Name = "x.txt"
		w.Comment = "c"
		w.Write(data)
		w.Close()
		r, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
		back, err2 := io.ReadAll(r)
		fmt.Println(lvl, buf.Len() > 0, r.Name, r.Comment, bytes.Equal(back, data), err, err2, buf.Len() < len(data) || lvl == 0)
	}
	var zb bytes.Buffer
	zw := zlib.NewWriter(&zb)
	zw.Write(data)
	zw.Close()
	fmt.Printf("%x\n", zb.Bytes()[:2])
	zr, err := zlib.NewReader(&zb)
	zback, _ := io.ReadAll(zr)
	fmt.Println(err, bytes.Equal(zback, data))
	var fb bytes.Buffer
	fw, _ := flate.NewWriterDict(&fb, 9, []byte("marmot compression"))
	fw.Write(data)
	fw.Close()
	fr := flate.NewReaderDict(&fb, []byte("marmot compression"))
	fback, err := io.ReadAll(fr)
	fmt.Println(err, bytes.Equal(fback, data))
	_, err = gzip.NewReader(bytes.NewReader([]byte("not gzip")))
	fmt.Println(err)
	bad := []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 0xff, 1, 2, 3}
	r2, _ := gzip.NewReader(bytes.NewReader(bad))
	_, err = io.ReadAll(r2)
	fmt.Println(err != nil)
	var lb bytes.Buffer
	lw := lzw.NewWriter(&lb, lzw.LSB, 8)
	lw.Write(data[:1000])
	lw.Close()
	lr := lzw.NewReader(&lb, lzw.LSB, 8)
	lback, _ := io.ReadAll(lr)
	fmt.Println(bytes.Equal(lback, data[:1000]))
	_, err = io.ReadAll(bzip2.NewReader(strings.NewReader("BZh9garbage")))
	fmt.Println(err != nil)
	var multi bytes.Buffer
	for _, s := range []string{"first ", "second"} {
		w := gzip.NewWriter(&multi)
		w.Write([]byte(s))
		w.Close()
	}
	mr, _ := gzip.NewReader(&multi)
	all, _ := io.ReadAll(mr)
	fmt.Println(string(all))
}
