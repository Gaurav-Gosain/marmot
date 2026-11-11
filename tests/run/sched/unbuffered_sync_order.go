package main

func main() {
	c := make(chan int)
	done := make(chan bool)
	go func() {
		println("recv start")
		v := <-c
		println("received", v)
		done <- true
	}()
	println("sending")
	c <- 5
	<-done
	println("end")
}
