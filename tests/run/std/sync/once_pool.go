package main

import (
	"sync"
	"sync/atomic"
)

func main() {
	var once sync.Once
	n := 0
	for i := 0; i < 5; i++ {
		once.Do(func() { n++ })
	}
	println(n)
	var wg sync.WaitGroup
	var cnt int64
	var c32 atomic.Int32
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt64(&cnt, 2)
			c32.Add(1)
		}()
	}
	wg.Wait()
	println(atomic.LoadInt64(&cnt), c32.Load())
	println(atomic.CompareAndSwapInt64(&cnt, 100, 7), cnt, atomic.CompareAndSwapInt64(&cnt, 100, 8))
	var v atomic.Value
	v.Store("x")
	println(v.Load().(string))
	p := sync.Pool{New: func() any { return new(int) }}
	x := p.Get().(*int)
	*x = 5
	p.Put(x)
	println(p.Get() != nil)
	ov := sync.OnceValue(func() int { n++; return n })
	println(ov(), ov())
}
