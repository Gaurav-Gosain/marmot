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
func f(c int) map[string][]int {
	m := map[string][]int{}
	v := mk(2, c)
	m["old"] = v
	v = append(v, 9, 9, 9, 9)
	m["new"] = v
	junk(c)
	junk(len(v))
	return m
}

func main() {
	for _, c := range []int{2, 4} {
		m := f(c)
		fmt.Println(m["old"], m["new"])
	}
}
