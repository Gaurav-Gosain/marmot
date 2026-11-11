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
func f(c int) [3][]int {
	var a [3][]int
	v := mk(2, c)
	a[0] = v
	v = append(v, 1, 2, 3)
	a[1] = v
	a[2] = v[1:3]
	junk(c)
	junk(len(v))
	return a
}

func main() {
	for _, c := range []int{2, 3, 5} {
		fmt.Println(f(c))
	}
}
