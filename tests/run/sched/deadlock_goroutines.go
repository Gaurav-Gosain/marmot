package main

func main() {
	a, b := make(chan int), make(chan int)
	go func() { <-a; b <- 1 }()
	println("waiting")
	<-b
}
