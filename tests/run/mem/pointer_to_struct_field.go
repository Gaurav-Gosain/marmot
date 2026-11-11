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

type pt struct{ x, y int }

//go:noinline
func f(c int) *int {
	v := make([]pt, 2, c)
	v[0], v[1] = pt{1, 2}, pt{3, 4}
	p := &v[1].y
	w := append(v, pt{5, 6}, pt{7, 8}, pt{9, 10})
	junk(c * 2)
	junk(len(w) * 2)
	return p
}

func main() {
	for _, c := range []int{2, 3, 8} {
		p := f(c)
		junk(c)
		fmt.Println(*p)
	}
}
