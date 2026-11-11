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
func f(c int) (int, int, int) {
	v := mk(0, c)
	a := append(v, 1)
	b := append(v, 2)
	junk(c)
	return a[0], b[0], len(v)
}

func main() {
	for _, c := range []int{2, 4} {
		fmt.Println(f(c))
	}
}
