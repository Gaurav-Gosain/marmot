package main

import (
	"runtime"
	"sync/atomic"
)

// The goroutine must get to run while main only yields. Another P can steal
// it and its thread can be slow to run under load, so main yields until the
// goroutine has counted a few rounds, with a bound far above what is needed.
func main() {
	var stop atomic.Bool
	var cnt atomic.Int64
	done := make(chan bool)
	go func() {
		for !stop.Load() {
			runtime.Gosched()
			cnt.Add(1)
		}
		done <- true
	}()
	for i := 0; i < 10000000 && cnt.Load() < 10; i++ {
		runtime.Gosched()
	}
	stop.Store(true)
	<-done
	println(cnt.Load() > 0)
}
