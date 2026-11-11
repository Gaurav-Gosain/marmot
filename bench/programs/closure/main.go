// closure calls through func values with captured state.
package main

const iters = 900000000

func counter(step int) func(int) int {
	total := 0
	return func(x int) int {
		total += x + step
		return total
	}
}

func main() {
	fs := []func(int) int{counter(1), counter(2), counter(3), counter(5)}
	sum := 0
	for i := 0; i < iters; i++ {
		sum ^= fs[i&3](i & 15)
	}
	println("closure", sum)
}
