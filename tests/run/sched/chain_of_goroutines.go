package main

func main() {
	const n = 3000
	leftmost := make(chan int)
	right := leftmost
	left := leftmost
	for i := 0; i < n; i++ {
		right = make(chan int)
		go func(l, r chan int) { l <- 1 + <-r }(left, right)
		left = right
	}
	go func(c chan int) { c <- 1 }(right)
	println(<-leftmost)
}
