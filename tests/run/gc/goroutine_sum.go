// 100000 goroutines, each with a captured loop variable and an
// allocation: the sum is right only if no captured variable is freed
// while its goroutine waits to run (wasm gcdata with 4-byte pointers).
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

var sink atomic.Pointer[[]byte]

func main() {
	const n = 100000
	var wg sync.WaitGroup
	var sum atomic.Int64
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := make([]byte, 64)
			sink.Store(&b)
			sum.Add(int64(i))
		}()
	}
	wg.Wait()
	fmt.Println("sum ok:", sum.Load() == int64(n)*(n-1)/2)
}
