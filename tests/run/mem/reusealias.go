package main

import "fmt"

//go:noinline
func f(c int) int {
	v := make([]int, 1, c) // v and a1 share one backing array
	v[0] = 42
	a1 := append(v, 7)  // fits: same array
	a2 := append(a1, 8) // grows: the old array (v's) is freed
	junk := make([]int, c)
	junk[0], junk[1] = -1, -1 // may reuse the freed array
	return v[0] + 0*len(a2) + 0*junk[0]
}

func main() { fmt.Println(f(2)) }
