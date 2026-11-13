// cgo: the collector runs while goroutines are in C, on the blocking and
// on the fast path, and Go memory passed to C stays valid.
package main

/*
#include <unistd.h>
static long slowsum(const unsigned char *p, long n) {
	long s = 0;
	for (long i = 0; i < n; i++) {
		s += p[i];
		if (i % (n / 4) == 0) usleep(2000);
	}
	return s;
}
static long spin(long n) { volatile long s = 0; for (long i = 0; i < n; i++) s += i & 7; return s; }
#cgo noescape spin
#cgo nocallback spin
*/
import "C"

import (
	"fmt"
	"runtime"
	"sync"
)

var sink [][]byte

func main() {
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			sink = append(sink, make([]byte, 64<<10))
			if len(sink) > 64 {
				sink = nil
				runtime.GC()
			}
		}
	}()
	var results [4]int64
	var workers sync.WaitGroup
	for g := 0; g < 4; g++ {
		workers.Add(1)
		go func(g int) {
			defer workers.Done()
			buf := make([]byte, 1<<20)
			for i := range buf {
				buf[i] = byte(i + g)
			}
			var want int64
			for _, b := range buf {
				want += int64(b)
			}
			got := int64(C.slowsum((*C.uchar)(&buf[0]), C.long(len(buf))))
			if got == want {
				results[g] = 1
			}
			for i := 0; i < 20; i++ {
				C.spin(1 << 20)
			}
		}(g)
	}
	workers.Wait()
	close(done)
	wg.Wait()
	fmt.Println(results)
}
