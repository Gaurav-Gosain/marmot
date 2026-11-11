package main

import "fmt"

//go:noinline
func f(c int) int {
	v := make([]int, 1, c)
	v[0] = 42
	p := &v[0]              // element address taken before the growth
	a := append(v, 1, 2, 3) // grows: v's array is freed
	junk := make([]int, c)
	junk[0] = -1
	return *p + 0*len(a) + 0*junk[0] // reads the freed array through p
}

func main() { fmt.Println(f(1)) }
