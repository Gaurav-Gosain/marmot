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

type holder struct {
	name string
	data []int
}

//go:noinline
func f(c int) holder {
	v := mk(2, c)
	h := holder{"h", v}
	v = append(v, 1, 2, 3, 4, 5)
	junk(c)
	junk(c + 5)
	_ = v
	return h
}

func main() {
	for _, c := range []int{2, 3, 6} {
		h := f(c)
		fmt.Println(h.name, h.data, len(h.data), cap(h.data) >= 2)
	}
}
