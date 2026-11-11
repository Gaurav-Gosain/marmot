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
func f(c int) [][]int {
	rows := make([][]int, 4)
	for i := range rows {
		rows[i] = mk(2, c)
	}
	old := rows[1]
	rows[1] = append(rows[1], 1, 2, 3, 4)
	junk(c)
	junk(len(rows[1]))
	return [][]int{old, rows[1], rows[2]}
}

func main() {
	for _, c := range []int{2, 4} {
		fmt.Println(f(c))
	}
}
