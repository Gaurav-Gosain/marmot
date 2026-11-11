package main

func main() {
	const n = 10000
	done := make(chan int, n)
	for i := 0; i < n; i++ {
		go func() { done <- i }()
	}
	sum := 0
	for i := 0; i < n; i++ {
		sum += <-done
	}
	println(sum)
}
