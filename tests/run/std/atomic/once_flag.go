package main

import (
	"sync"
	"sync/atomic"
)

func main() {
	var inited atomic.Bool
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !inited.Load() {
				if inited.CompareAndSwap(false, true) {
					count.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	println(count.Load(), inited.Load())
}
