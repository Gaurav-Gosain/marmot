package main

import (
	"fmt"
	"sync"
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

var pool = sync.Pool{New: func() any { return make([]int, 2, 4) }}

//go:noinline
func f() ([]int, []int) {
	a := pool.Get().([]int)
	a[0], a[1] = 7, 8
	b := append(a, 9, 10, 11)
	pool.Put(a[:0])
	junk(4)
	junk(len(b))
	return a, b
}

func main() {
	for i := 0; i < 3; i++ {
		a, b := f()
		fmt.Println(a, b)
	}
}
