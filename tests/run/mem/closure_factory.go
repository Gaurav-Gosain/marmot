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
func counter(c int) (func(int), func() []int) {
	v := mk(1, c)
	return func(x int) { v = append(v, x); junk(c + x) }, func() []int { return v }
}

func main() {
	for _, c := range []int{1, 2, 5} {
		add, get := counter(c)
		first := get()
		for i := 0; i < 6; i++ {
			add(i)
		}
		fmt.Println(first, get())
	}
}
