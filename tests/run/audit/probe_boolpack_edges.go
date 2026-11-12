// Edge cases for backend optimizations; the expected output is from gc.
package main

import (
	"fmt"
	"sync"
)

//go:noinline
func sieve(n int) int {
	comp := make([]bool, n+1)
	c := 0
	for i := 2; i <= n; i++ {
		if !comp[i] {
			c++
			for j := i * i; j <= n; j += i {
				comp[j] = true
			}
		}
	}
	return c
}

//go:noinline
func concurrent(n int) int {
	// Distinct elements written by different goroutines: not a race in Go.
	b := make([]bool, n)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := g; i < n; i += 8 {
				for k := 0; k < 50; k++ {
					b[i] = !b[i]
				}
				b[i] = true
			}
		}(g)
	}
	wg.Wait()
	c := 0
	for _, v := range b {
		if v {
			c++
		}
	}
	return c
}

//go:noinline
func lenCap(n int) (int, int, bool) {
	b := make([]bool, n, n+9)
	b[n-1] = true
	return len(b), cap(b), b[n-1]
}

//go:noinline
func oob(n, i int) bool {
	b := make([]bool, n)
	b[i] = true // n=9, i=12 is in the packed bytes but out of range
	return b[i]
}

//go:noinline
func neg(n int) bool {
	b := make([]bool, n)
	return b[0]
}

//go:noinline
func addr(n int) bool {
	b := make([]bool, n)
	p := &b[3]
	*p = true
	return b[3]
}

//go:noinline
func copyOut(n int) []bool {
	b := make([]bool, n)
	b[1] = true
	c := make([]bool, n)
	copy(c, b)
	return c
}

//go:noinline
func rangeVal(n int) int {
	b := make([]bool, n)
	for i := range b {
		b[i] = i%3 == 0
	}
	c := 0
	for _, v := range b {
		if v {
			c++
		}
	}
	return c
}

//go:noinline
func storeByte(n int) bool {
	b := make([]bool, n)
	x := true
	b[5] = x
	b[6] = !b[5]
	return b[5] && !b[6]
}

func main() {
	fmt.Println(sieve(100000))
	fmt.Println(concurrent(4096))
	fmt.Println(lenCap(10))
	fmt.Println(copyOut(4), rangeVal(10), storeByte(8), addr(5))
	func() {
		defer func() { fmt.Println("oob:", recover()) }()
		fmt.Println(oob(9, 12))
	}()
	func() {
		defer func() { fmt.Println("neg:", recover()) }()
		fmt.Println(neg(-1))
	}()
}
