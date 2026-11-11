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
func f(c int) []int {
	ch := make(chan []int, 3)
	v := mk(2, c)
	ch <- v
	v = append(v, 1, 2, 3)
	ch <- v
	junk(c)
	junk(c * 3)
	a, b := <-ch, <-ch
	return append(append([]int{}, a...), b...)
}

func main() {
	for _, c := range []int{2, 3, 5} {
		fmt.Println(f(c))
	}
}
