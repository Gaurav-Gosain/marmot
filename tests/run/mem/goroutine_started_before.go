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
func f(c int) int {
	v := mk(2, c)
	start := make(chan bool)
	res := make(chan int)
	go func() {
		<-start
		res <- v[0] + v[1]
	}()
	w := append(v, 1, 2, 3, 4)
	junk(c)
	junk(len(w))
	start <- true
	return <-res + 0*len(w)
}

func main() {
	for _, c := range []int{2, 3, 5} {
		fmt.Println(f(c))
	}
}
