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
func f(c int) int {
	v := mk(1, c)
	v[0] = 42
	a1 := append(v, 7)
	a2 := append(a1, 8)
	a3 := append(a2, 9)
	a4 := append(a3, 10)
	for i := 0; i < 4; i++ {
		junk(c)
		junk(c * 2)
	}
	return v[0]*1000000 + a1[1]*10000 + a2[2]*100 + a4[4] + 0*len(a3)
}

func main() {
	for c := 1; c <= 10; c++ {
		fmt.Println(c, f(c))
	}
}
