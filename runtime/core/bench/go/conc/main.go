// Concurrency micro benchmarks, built by both go and marmot:
//   go build -o conc_go . ; marmot build -O 2 -o conc_marmot .
// Prints ns per operation for each case.
package main

import (
	"sync"
	"time"
)

// println only: no os or strconv, so the program links where the os
// overlay is not ready (linux).
func report(name string, n int, d time.Duration) {
	println(name+":", d.Nanoseconds()*10/int64(n), "x0.1 ns/op")
}

func spawn(n int) time.Duration {
	var wg sync.WaitGroup
	t := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { wg.Done() }()
	}
	wg.Wait()
	return time.Since(t)
}

func unbuffered(n int) time.Duration {
	ping, pong := make(chan int), make(chan int)
	go func() {
		for v := range ping {
			pong <- v
		}
		close(pong)
	}()
	t := time.Now()
	for i := 0; i < n; i++ {
		ping <- i
		<-pong
	}
	d := time.Since(t)
	close(ping)
	return d
}

func buffered(n int) time.Duration {
	c := make(chan int, 1024)
	done := make(chan int)
	go func() {
		s := 0
		for v := range c {
			s += v
		}
		done <- s
	}()
	t := time.Now()
	for i := 0; i < n; i++ {
		c <- i
	}
	close(c)
	<-done
	return time.Since(t)
}

func selectTwo(n int) time.Duration {
	a, b := make(chan int, 64), make(chan int, 64)
	quit := make(chan bool)
	go func() {
		for i := 0; i < n; i++ {
			if i&1 == 0 {
				a <- i
			} else {
				b <- i
			}
		}
		quit <- true
	}()
	t := time.Now()
	got := 0
	for got < n {
		select {
		case <-a:
			got++
		case <-b:
			got++
		}
	}
	<-quit
	return time.Since(t)
}

func mutex(n, workers int) time.Duration {
	var mu sync.Mutex
	var wg sync.WaitGroup
	counter := 0
	t := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			for i := 0; i < n/workers; i++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
			wg.Done()
		}()
	}
	wg.Wait()
	return time.Since(t)
}

func fanout(rounds, width int) time.Duration {
	data := make([]int, width)
	t := time.Now()
	for r := 0; r < rounds; r++ {
		var wg sync.WaitGroup
		wg.Add(width)
		for i := 0; i < width; i++ {
			go func(i int) {
				data[i] += i
				wg.Done()
			}(i)
		}
		wg.Wait()
	}
	return time.Since(t)
}

func main() {
	report("spawn+exit (WaitGroup)", 1000000, spawn(1000000))
	report("unbuffered chan round trip", 1000000, unbuffered(1000000))
	report("buffered chan send+recv (cap 1024)", 5000000, buffered(5000000))
	report("select over 2 buffered chans", 2000000, selectTwo(2000000))
	report("mutex lock+unlock, 4 goroutines", 4000000, mutex(4000000, 4))
	report("WaitGroup fan-out of 16 goroutines (per round)", 50000, fanout(50000, 16))
}
