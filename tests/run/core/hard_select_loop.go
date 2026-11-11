package main

func main() {
	a, b, quit := make(chan int), make(chan int), make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			a <- i
		}
		for i := 0; i < 50; i++ {
			b <- i
		}
		close(quit)
	}()
	sa, sb, n := 0, 0, 0
	for {
		select {
		case v := <-a:
			sa += v
			n++
		case v := <-b:
			sb += v
			n++
		case <-quit:
			println(sa, sb, n)
			return
		}
	}
}
