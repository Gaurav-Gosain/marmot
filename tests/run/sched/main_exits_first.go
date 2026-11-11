package main

func main() {
	block := make(chan int)
	go func() {
		<-block
		println("never printed")
	}()
	go func() {
		for {
		}
	}()
	println("main done")
}
