// fib: recursive calls. Checksum is fib(n).
package main

const n = 43

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	println("fib", n, fib(n))
}
