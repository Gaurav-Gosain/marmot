package main

func main() {
	done := make(chan bool)
	go func() {
		println("in goroutine")
		done <- true
	}()
	<-done
	res := make(chan int)
	for i := 0; i < 3; i++ {
		go func() { res <- i * i }()
	}
	t := 0
	for i := 0; i < 3; i++ {
		t += <-res
	}
	println(t)
	go func(a, b int) { done <- a+b == 3 }(1, 2)
	println(<-done)
}
