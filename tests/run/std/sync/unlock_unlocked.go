package main

import "sync"

func main() {
	var mu sync.Mutex
	println("start")
	mu.Unlock()
	println("unreachable")
}
