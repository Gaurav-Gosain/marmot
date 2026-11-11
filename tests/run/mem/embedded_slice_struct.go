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

type base struct{ items []int }
type derived struct {
	base
	extra []int
}

//go:noinline
func f(c int) derived {
	d := derived{base{mk(2, c)}, mk(1, c)}
	d.items = append(d.items, 1, 2, 3)
	d2 := d
	d2.items = append(d2.items, 4, 5, 6, 7)
	junk(c)
	junk(len(d2.items))
	return d
}

func main() {
	for _, c := range []int{2, 5} {
		d := f(c)
		fmt.Println(d.items, d.extra)
	}
}
