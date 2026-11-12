// Edge cases for backend optimizations; the expected output is from gc.
package main

import "fmt"

// new([8000]byte) does not escape: go-escape moves it to the stack
// (<= 8192 bytes). Go grows goroutine stacks; deep recursion must work.
//
//go:noinline
func rec(n int) int {
	p := new([8000]byte)
	p[n%8000] = byte(n)
	if n == 0 {
		return int(p[0])
	}
	return rec(n-1) + int(p[n%8000])
}

func main() {
	fmt.Println(rec(5000))
	done := make(chan int)
	go func() { done <- rec(5000) }()
	fmt.Println(<-done)
}
