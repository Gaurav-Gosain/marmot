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
func f() (int, int, int) {
	v := make([]int, 1, 50000)
	v[0] = 42
	a := append(v, 7)
	b := append(a, make([]int, 60000)...)
	big := junk(50000)
	big2 := junk(120000)
	return v[0], a[1] + 0*len(b), big[0] + big2[1]
}

func main() {
	for i := 0; i < 3; i++ {
		fmt.Println(f())
	}
}
