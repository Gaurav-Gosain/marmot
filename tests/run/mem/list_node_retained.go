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

type item struct {
	v    int
	next *item
	buf  []int
}

//go:noinline
func build(n, c int) (head, mid *item) {
	for i := n; i > 0; i-- {
		head = &item{i, head, mk(2, c)}
		if i == n/2 {
			mid = head
		}
	}
	return
}

func main() {
	for _, c := range []int{2, 4} {
		_, mid := build(20, c)
		for i := 0; i < 6; i++ {
			junk(c * 8)
			build(20, c)
		}
		s := 0
		for n := mid; n != nil; n = n.next {
			s += n.v + n.buf[1]
		}
		fmt.Println(mid.v, s)
	}
}
