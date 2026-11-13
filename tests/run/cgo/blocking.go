// cgo: a C call that blocks gives up its P: other goroutines run meanwhile,
// even with GOMAXPROCS(1).
package main

/*
#include <unistd.h>
static void nap(int ms) { usleep(ms * 1000); }
*/
import "C"

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	runtime.GOMAXPROCS(1)
	var ticks atomic.Int64
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				ticks.Add(1)
				time.Sleep(time.Millisecond)
			}
		}
	}()
	time.Sleep(5 * time.Millisecond)
	before := ticks.Load()
	C.nap(100)
	during := ticks.Load() - before
	close(stop)
	fmt.Println("other goroutine ran during the C call:", during > 10)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			C.nap(100)
		}()
	}
	wg.Wait()
	fmt.Println("8 naps of 100ms overlapped:", time.Since(start) < 600*time.Millisecond)
}
