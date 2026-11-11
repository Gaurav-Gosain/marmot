package main

import (
	"fmt"
	"reflect"
)

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
func f(c int) reflect.Value {
	v := mk(3, c)
	r := reflect.ValueOf(v)
	w := append(v, 1, 2, 3, 4)
	junk(c)
	junk(len(w))
	return r
}

func main() {
	for _, c := range []int{3, 4, 7} {
		r := f(c)
		junk(c)
		fmt.Println(r.Len(), r.Index(0), r.Index(2), r.Interface())
	}
}
