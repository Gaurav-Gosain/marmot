// select heavy pipeline: producer, two filters merged with select, and a sink.
package main

const n = 3000000

func main() {
	a := make(chan int)
	b := make(chan int)
	quit := make(chan bool)
	out := make(chan int)
	go func() {
		for i := 0; i < n; i++ {
			if i%2 == 0 {
				a <- i
			} else {
				b <- i
			}
		}
		close(quit)
	}()
	go func() {
		for {
			select {
			case v := <-a:
				out <- v * 3
			case v := <-b:
				out <- v * 5
			case <-quit:
				close(out)
				return
			}
		}
	}()
	sum, cnt := 0, 0
	for v := range out {
		sum += v
		cnt++
	}
	println("selectpipe", cnt, sum)
}
