package main

func gen(n int) <-chan int {
	c := make(chan int)
	go func() {
		for i := 1; i <= n; i++ {
			c <- i
		}
		close(c)
	}()
	return c
}

func sq(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		for v := range in {
			out <- v * v
		}
		close(out)
	}()
	return out
}

func add1(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		for v := range in {
			out <- v + 1
		}
		close(out)
	}()
	return out
}

func main() {
	t := 0
	for v := range add1(sq(add1(gen(1000)))) {
		t += v
	}
	println(t)
}
