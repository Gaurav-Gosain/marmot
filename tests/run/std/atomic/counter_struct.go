package main

import (
	"sync"
	"sync/atomic"
)

type Stats struct {
	hits, misses atomic.Int64
	last         atomic.Value
}

func main() {
	var s Stats
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if i%4 == 0 {
					s.misses.Add(1)
				} else {
					s.hits.Add(1)
				}
				s.last.Store(g)
			}
		}()
	}
	wg.Wait()
	println(s.hits.Load(), s.misses.Load(), s.last.Load() != nil)
}
