package main

func try(f func()) {
	defer func() { println(recover().(error).Error()) }()
	f()
}

func main() {
	c := make(chan int, 1)
	close(c)
	try(func() { close(c) })
	try(func() { c <- 1 })
	var n chan int
	try(func() { close(n) })
	v, ok := <-c
	println(v, ok)
}
