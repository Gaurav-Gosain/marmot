package main

import (
	"sync"
	"sync/atomic"
)

func main() {
	var i32 int32
	var i64 int64
	var u32 uint32
	var u64 uint64
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				atomic.AddInt32(&i32, 1)
				atomic.AddInt64(&i64, 2)
				atomic.AddUint32(&u32, 3)
				atomic.AddUint64(&u64, 4)
			}
		}()
	}
	wg.Wait()
	println(i32, i64, u32, u64)
	println(atomic.AddInt32(&i32, -8000), atomic.AddUint32(&u32, ^uint32(0)))
	println(atomic.LoadInt32(&i32), atomic.LoadUint64(&u64))
}
