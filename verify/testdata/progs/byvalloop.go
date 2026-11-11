package main

import "fmt"

//go:noinline
func mk(n int) []func() int {
	var fs []func() int
	var x int // one variable shared by every closure
	for i := 0; i < n; i++ {
		x = i * 10
		fs = append(fs, func() int { return x })
	}
	return fs
}

func main() {
	for _, f := range mk(3) {
		fmt.Print(f(), " ")
	}
	fmt.Println()
}
