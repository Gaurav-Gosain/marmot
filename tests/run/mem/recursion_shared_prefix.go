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
func gen(d int, prefix []int, out *[][]int) {
	if d == 0 {
		*out = append(*out, prefix)
		return
	}
	gen(d-1, append(prefix, d), out)
	gen(d-1, append(prefix, -d), out)
	junk(d)
}

func main() {
	for _, c := range []int{1, 2, 4} {
		var out [][]int
		gen(3, mk(0, c), &out)
		fmt.Println(out)
	}
}
