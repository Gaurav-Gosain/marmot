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
func l3(c int) []int { return mk(2, c) }

//go:noinline
func l2(c int) ([]int, []int) {
	v := l3(c)
	w := append(v, 5, 6, 7)
	junk(c)
	return v, w
}

//go:noinline
func l1(c int) (int, []int) {
	a, b := l2(c)
	junk(len(b))
	return len(b), a
}

func main() {
	for _, c := range []int{2, 3, 6} {
		n, a := l1(c)
		junk(c)
		fmt.Println(n, a)
	}
}
