package main

import (
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
	var sb strings.Builder
	sb.Grow(c)
	sb.WriteString("hello")
	s1 := sb.String()
	sb.WriteString(strings.Repeat("!", 200))
	junk(c)
	s2 := sb.String()
	return s1, s2[:8]
}

func main() {
	for _, c := range []int{5, 10, 100} {
		fmt.Println(f(c))
	}
}
