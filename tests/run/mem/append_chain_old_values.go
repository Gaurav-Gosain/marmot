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
	v := mk(1, c)
	a1 := append(v, 7)
	a2 := append(a1, 8)
	a3 := append(a2, 9)
	a4 := append(a3, 10)
	junk(c)
	junk(c * 2)
	junk(c * 4)
	return []int{v[0], a1[0], a1[1], a2[0], a2[2], a3[3], a4[0], a4[4], len(a4)}
}

func main() {
	for _, c := range []int{1, 2, 3, 4, 8} {
		fmt.Println(f(c))
	}
}
