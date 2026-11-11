package main

import (
	"bufio"
	"os"
	"strings"
)

func main() {
	sc := bufio.NewScanner(strings.NewReader("line one\nline two\n\nlast"))
	n := 0
	for sc.Scan() {
		n++
		println(n, sc.Text())
	}
	ws := bufio.NewScanner(strings.NewReader("a bb  ccc\nd"))
	ws.Split(bufio.ScanWords)
	cnt := 0
	for ws.Scan() {
		cnt += len(ws.Text())
	}
	println(cnt)
	rd := bufio.NewReader(strings.NewReader("x,y;z"))
	s, _ := rd.ReadString(',')
	b, _ := rd.ReadByte()
	println(s, b)
	w := bufio.NewWriter(os.Stdout)
	w.WriteString("buffered out\n")
	println(w.Buffered())
	w.Flush()
	println(w.Buffered())
}
