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
func f(n int) (byte, byte, int) {
	v := make([]byte, 1, n)
	v[0] = 'A'
	a := append(v, 'B')
	b := append(a, make([]byte, n)...)
	j := make([]byte, n)
	for i := range j {
		j[i] = 'Z'
	}
	sink = append(sink, []int{int(j[0])})
	return v[0], a[1], len(b)
}

func main() {
	for _, n := range []int{1, 7, 8, 9, 15, 16, 17, 31, 32, 33, 64, 100, 255, 256, 1000, 4096, 5000, 70000} {
		fmt.Println(f(n))
	}
}
