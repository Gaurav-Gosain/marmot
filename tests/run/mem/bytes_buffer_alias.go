package main

import (
	"bytes"
	"fmt"
	"strings"
)

var sink [][]int

//go:noinline
func junk(n int) []int {
	j := make([]int, n)
	for i := range j {
		j[i] = -1 - i
	}
	sink = append(sink, j)
	if len(sink) > 8 {
		sink = sink[1:]
	}
	return j
}

//go:noinline
func mk(n, c int) []int {
	v := make([]int, n, c)
	for i := range v {
		v[i] = 100 + i
	}
	return v
}

var _ = fmt.Sprint

//go:noinline
func f(c int) (string, string) {
	var b bytes.Buffer
	b.Grow(c)
	b.WriteString("abc")
	old := b.Bytes()
	b.WriteString(strings.Repeat("z", 100))
	junk(c)
	s := string(old)
	return s, string(b.Bytes()[:5])
}

func main() {
	for _, c := range []int{3, 8, 64} {
		fmt.Println(f(c))
	}
}
