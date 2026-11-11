package main

import (
	"fmt"
	"slices"
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
func f(c int) ([]int, []int, []int) {
	v := mk(3, c)
	g := slices.Grow(v, 20)
	g = append(g, 1)
	ins := slices.Insert(v, 1, 50, 60)
	junk(c)
	junk(len(ins))
	cl := slices.Clone(v)
	return v, ins, append(cl, g...)
}

func main() {
	for _, c := range []int{3, 4, 10} {
		fmt.Println(f(c))
	}
}
