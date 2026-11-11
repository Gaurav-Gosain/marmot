package main

import "time"

func slow(d time.Duration) <-chan string {
	c := make(chan string, 1)
	go func() {
		time.Sleep(d)
		c <- "done"
	}()
	return c
}

func main() {
	select {
	case r := <-slow(5 * time.Millisecond):
		println(r)
	case <-time.After(500 * time.Millisecond):
		println("timeout")
	}
	select {
	case r := <-slow(500 * time.Millisecond):
		println(r)
	case <-time.After(10 * time.Millisecond):
		println("timeout")
	}
	order := make(chan int, 3)
	for _, d := range []int{30, 10, 20} {
		go func() {
			time.Sleep(time.Duration(d) * time.Millisecond)
			order <- d
		}()
	}
	println(<-order, <-order, <-order)
}
