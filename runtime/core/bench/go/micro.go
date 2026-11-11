// Go counterparts of ../micro.c. Run: GOMAXPROCS=1 go run micro.go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

type obj16 struct{ a, b *int }
type obj64 struct {
	a, b *int
	pad  [6]int
}
type obj256 struct {
	a, b *int
	pad  [30]int
}
type obj1024 struct {
	a, b *int
	pad  [126]int
}
type small32 struct{ x [4]int }

var sink interface{}

func main() {
	var stop int32
	var yields int64
	go func() {
		for stop == 0 {
			yields++
			runtime.Gosched()
		}
	}()
	n := 2000000
	t0 := time.Now()
	for i := 0; i < n; i++ {
		runtime.Gosched()
	}
	d := time.Since(t0)
	stop = 1
	fmt.Printf("gosched round trip (2 goroutines): %.1f ns per yield\n", float64(d.Nanoseconds())/float64(int64(n)+yields))

	ping, pong := make(chan struct{}), make(chan struct{})
	go func() {
		for range ping {
			pong <- struct{}{}
		}
	}()
	n = 1000000
	t0 = time.Now()
	for i := 0; i < n; i++ {
		ping <- struct{}{}
		<-pong
	}
	d = time.Since(t0)
	fmt.Printf("unbuffered channel ping-pong: %.1f ns per switch\n", float64(d.Nanoseconds())/float64(2*n))

	var wg sync.WaitGroup
	n = 1000000
	wg.Add(n)
	t0 = time.Now()
	for i := 0; i < n; i++ {
		go wg.Done()
	}
	wg.Wait()
	d = time.Since(t0)
	fmt.Printf("goroutine spawn+exit: %.1f ns each\n", float64(d.Nanoseconds())/float64(n))

	bench := func(name string, n int, f func()) {
		t0 := time.Now()
		for i := 0; i < n; i++ {
			f()
		}
		fmt.Printf("alloc %s: %.1f ns each\n", name, float64(time.Since(t0).Nanoseconds())/float64(n))
	}
	bench("  16 B (2 ptr words)", 20000000, func() { o := &obj16{}; o.a = new(int); sink = o })
	bench("  64 B", 10000000, func() { o := &obj64{}; sink = o })
	bench(" 256 B", 6666666, func() { o := &obj256{}; sink = o })
	bench("1024 B", 5000000, func() { o := &obj1024{}; sink = o })
	bench("  32 B (noscan)", 20000000, func() { o := &small32{}; sink = o })
}
