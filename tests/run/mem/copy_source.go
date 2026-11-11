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
func f(c int) []int {
	v := mk(3, c)
	w := append(v, 1, 2, 3, 4, 5)
	junk(c)
	junk(len(w))
	dst := make([]int, 3)
	copy(dst, v)
	return dst
}

func main() {
	for _, c := range []int{3, 4, 9} {
		fmt.Println(f(c))
	}
}
