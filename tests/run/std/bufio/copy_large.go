package main

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

func main() {
	src := strings.Repeat("line of text\n", 20000)
	var out bytes.Buffer
	w := bufio.NewWriter(&out)
	r := bufio.NewReader(strings.NewReader(src))
	n, err := io.Copy(w, r)
	w.Flush()
	println(n, err == nil, out.Len() == len(src))
	sc := bufio.NewScanner(&out)
	lines := 0
	for sc.Scan() {
		lines++
	}
	println(lines)
}
