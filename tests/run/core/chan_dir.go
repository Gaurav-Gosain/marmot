package main

func producer(out chan<- int, n int) {
	for i := 0; i < n; i++ {
		out <- i
	}
	close(out)
}

func consumer(in <-chan int) int {
	t := 0
	for v := range in {
		t += v
	}
	return t
}

func main() {
	c := make(chan int, 2)
	go producer(c, 10)
	println(consumer(c))
	var r <-chan int = c
	var s chan<- int = c
	println(r == nil, s == nil)
	cc := make(chan chan int, 1)
	inner := make(chan int, 1)
	cc <- inner
	inner <- 7
	println(<-<-cc)
	fc := make(chan func() int, 1)
	fc <- func() int { return 11 }
	println((<-fc)())
}
