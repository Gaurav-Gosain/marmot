package main

import (
	"fmt"
	"runtime"
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

type res struct {
	data []int
	id   int
}

var finalized int

//go:noinline
func make1(i, c int) *res {
	r := &res{mk(3, c), i}
	runtime.SetFinalizer(r, func(x *res) { finalized++ })
	return r
}

//go:noinline
func f(c int) []int {
	r := make1(1, c)
	keep := r.data
	r = nil
	for i := 0; i < 5; i++ {
		runtime.GC()
		junk(c * 4)
	}
	return keep
}

func main() {
	for _, c := range []int{3, 5} {
		fmt.Println(f(c))
	}
	runtime.GC()
	fmt.Println(finalized <= 2)
}
