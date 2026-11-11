package main

func depth(n int) int {
	if n == 0 {
		return 0
	}
	var pad [16]int
	pad[n%16] = n
	return depth(n-1) + 1 + pad[0]*0
}

func main() {
	c := make(chan int)
	go func() { c <- depth(100000) }()
	println(<-c)
	println(depth(200000))
}
