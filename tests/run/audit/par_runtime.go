// Audit (loops, 2026-11-12): behavior of the parallel loop runtime under -par
// (lead conditions for the default flip). runtime.NumGoroutine right after a
// parallel loop must not count its helpers (gc prints 1; marmot with -par
// at GOMAXPROCS 8 printed 4: helpers still exiting); GOMAXPROCS=1 gives the
// serial result; a panic in a loop body is the first failing iteration in
// order with the prefix written. Only differs under the loops configs.
package main

import (
	"fmt"
	"runtime"
)

const N = 4000000

//go:noinline
func fill(a []int) {
	for i := range a {
		a[i] = i*7 + 3
	}
}

//go:noinline
func sum(a []int) int {
	s := 0
	for _, v := range a {
		s += v
	}
	return s
}

//go:noinline
func scale(dst, src []int, k int) {
	for i := range dst {
		dst[i] = src[i] * k
	}
}

func main() {
	a := make([]int, N)
	b := make([]int, N)
	maxG := 0
	for r := 0; r < 50; r++ {
		fill(a)
		// Helpers of the parallel loop must be gone when it returns.
		if g := runtime.NumGoroutine(); g > maxG {
			maxG = g
		}
		scale(b, a, r)
		if g := runtime.NumGoroutine(); g > maxG {
			maxG = g
		}
	}
	fmt.Println(sum(a), sum(b), "max goroutines seen:", maxG)

	// A panic in a loop body: the first failing iteration in order, with
	// the prefix written.
	c := make([]int, N)
	idx := make([]int, N)
	for i := range idx {
		idx[i] = i
	}
	idx[N-10] = N + 1
	idx[N/2] = -1
	func() {
		defer func() { fmt.Println("recovered:", recover(), c[N/2-1], c[N/2], c[N/2+1]) }()
		for i := range c {
			c[i] = a[idx[i]]
		}
	}()
}
