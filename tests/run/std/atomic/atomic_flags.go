package main

import (
	"sync"
	"sync/atomic"
)

func main() {
	var ready atomic.Bool
	var data atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !ready.Load() {
		}
		println("saw", data.Load())
	}()
	data.Store(123)
	ready.Store(true)
	wg.Wait()
	var once atomic.Bool
	wins := atomic.Int32{}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if once.CompareAndSwap(false, true) {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	println(wins.Load())
}
