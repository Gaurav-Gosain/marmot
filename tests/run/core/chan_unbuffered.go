package main

func main() {
	ch := make(chan int)
	go func() {
		for i := 0; i < 5; i++ {
			ch <- i
		}
		close(ch)
	}()
	for v := range ch {
		println(v)
	}
	v, ok := <-ch
	println(v, ok)
	ping, pong := make(chan int), make(chan int)
	go func() {
		for v := range ping {
			pong <- v + 1
		}
		close(pong)
	}()
	x := 0
	for i := 0; i < 100; i++ {
		ping <- x
		x = <-pong
	}
	close(ping)
	println(x)
}
