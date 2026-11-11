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

type S []int

func (s S) Sum() int {
	t := 0
	for _, x := range s {
		t += x
	}
	return t
}

//go:noinline
func f(c int) func() int {
	v := S(mk(3, c))
	m := v.Sum
	w := append(v, 1, 2, 3, 4)
	junk(c)
	junk(len(w))
	return m
}

func main() {
	for _, c := range []int{3, 4, 7} {
		m := f(c)
		junk(c)
		fmt.Println(m())
	}
}
