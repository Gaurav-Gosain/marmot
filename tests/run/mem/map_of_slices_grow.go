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
func f(c int) ([]int, []int) {
	m := map[int][]int{}
	m[1] = mk(2, c)
	old := m[1]
	for i := 0; i < 6; i++ {
		m[1] = append(m[1], i)
		junk(c + i)
	}
	return old, m[1]
}

func main() {
	for _, c := range []int{2, 3, 4} {
		fmt.Println(f(c))
	}
}
