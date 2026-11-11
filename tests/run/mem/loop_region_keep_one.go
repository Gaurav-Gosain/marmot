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
	var kept []int
	for i := 0; i < 8; i++ {
		v := mk(2, c)
		v[0] = i
		w := append(v, i, i)
		if i == 3 {
			kept = v
		}
		junk(c)
		_ = w
	}
	junk(c * 2)
	return kept
}

func main() {
	for _, c := range []int{2, 3, 8} {
		fmt.Println(f(c))
	}
}
