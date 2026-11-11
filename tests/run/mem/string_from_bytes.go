package main

import "fmt"

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
func f(c int) (string, string, int) {
	b := make([]byte, 5, c)
	copy(b, "hello")
	s := string(b)
	b2 := append(b, " world and more text"...)
	t := string(b2[:7])
	junk(c)
	for i := range b {
		b[i] = 'x'
	}
	return s, t, len(b2)
}

func main() {
	for _, c := range []int{5, 8, 40} {
		fmt.Println(f(c))
	}
}
