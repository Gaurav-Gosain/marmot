package main

func main() {
	println("before")
	c := make(chan int)
	<-c
	println("after")
}
