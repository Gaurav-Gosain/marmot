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

type lister interface{ List() []int }
type box struct{ items []int }

func (b *box) List() []int { return b.items }

//go:noinline
func f(c int) ([]int, lister) {
	b := &box{mk(2, c)}
	var l lister = b
	old := l.List()
	b.items = append(b.items, 1, 2, 3, 4)
	junk(c)
	junk(len(b.items))
	return old, l
}

func main() {
	for _, c := range []int{2, 3, 6} {
		old, l := f(c)
		junk(c)
		fmt.Println(old, l.List())
	}
}
