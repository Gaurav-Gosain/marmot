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

type job struct {
	id   int
	data []int
}

//go:noinline
func f(c int) int {
	jobs := make(chan job)
	out := make(chan int)
	for w := 0; w < 3; w++ {
		go func() {
			for j := range jobs {
				junk(c)
				s := 0
				for _, x := range j.data {
					s += x
				}
				out <- s + j.id
			}
		}()
	}
	t := 0
	v := mk(2, c)
	for i := 0; i < 6; i++ {
		go func() { jobs <- job{i, v} }()
		v = append(v, i)
		junk(len(v))
	}
	for i := 0; i < 6; i++ {
		t += <-out
	}
	close(jobs)
	return t
}

func main() {
	for _, c := range []int{2, 3, 8} {
		fmt.Println(f(c) > 0)
	}
}
