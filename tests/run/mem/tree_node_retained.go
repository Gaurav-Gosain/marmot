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

type node struct {
	val         int
	left, right *node
	data        []int
}

//go:noinline
func build(d int, c int) *node {
	if d == 0 {
		return &node{val: 1, data: mk(2, c)}
	}
	return &node{val: d, left: build(d-1, c), right: build(d-1, c), data: mk(1, c)}
}

//go:noinline
func f(c int) *node {
	t := build(4, c)
	n := t.left.right.left
	t = nil
	for i := 0; i < 5; i++ {
		junk(c * 4)
		build(3, c)
	}
	return n
}

func main() {
	for _, c := range []int{2, 4} {
		n := f(c)
		junk(100)
		fmt.Println(n.val, n.left.val, n.right.data, n.data)
	}
}
