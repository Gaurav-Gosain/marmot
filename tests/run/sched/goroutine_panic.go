package main

func main() {
	done := make(chan int)
	go func() {
		panic("goroutine panic")
	}()
	<-done
}
