package main

import (
	"runtime"
	"sync"
	"sync/atomic"
)

type SpinLock struct{ f atomic.Int32 }

func (s *SpinLock) Lock() {
	for !s.f.CompareAndSwap(0, 1) {
		runtime.Gosched()
	}
}
func (s *SpinLock) Unlock() { s.f.Store(0) }

func main() {
	var l SpinLock
	var wg sync.WaitGroup
	n := 0
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				l.Lock()
				n++
				l.Unlock()
			}
		}()
	}
	wg.Wait()
	println(n)
}
