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
	v := mk(4, c)
	sub := v[1:3]
	grown := append(v, 1, 2, 3, 4, 5, 6, 7, 8)
	junk(c)
	junk(len(grown))
	return sub[0], sub[1], len(grown)
}

func main() {
	for _, c := range []int{4, 5, 8, 16} {
		fmt.Println(f(c))
	}
}
