package main

func main() {
	a, b := make(chan int), make(chan int)
	go func() {
		for v := range a {
			b <- v + 1
		}
		close(b)
	}()
	v := 0
	for i := 0; i < 20000; i++ {
		a <- v
		v = <-b
	}
	close(a)
	_, ok := <-b
	println(v, ok)
}
