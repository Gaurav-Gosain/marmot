package main

func main() {
	a, b := make(chan int, 1), make(chan string, 1)
	a <- 1
	select {
	case v := <-a:
		println("a", v)
	case s := <-b:
		println("b", s)
	}
	b <- "x"
	select {
	case v := <-a:
		println("a", v)
	case s := <-b:
		println("b", s)
	}
	select {
	case v := <-a:
		println(v)
	default:
		println("default")
	}
	done := make(chan bool)
	data := make(chan int)
	go func() {
		for i := 0; i < 3; i++ {
			data <- i
		}
		close(done)
	}()
	for {
		select {
		case v := <-data:
			println("data", v)
		case <-done:
			println("done")
			return
		}
	}
}
