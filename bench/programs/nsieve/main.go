// nsieve: sieve of Eratosthenes on a byte slice, bounds checks in the hot loop.
package main

const size = 150000000

func sieve(n int) int {
	flags := make([]bool, n+1)
	count := 0
	for i := 2; i <= n; i++ {
		if !flags[i] {
			count++
			for j := i + i; j <= n; j += i {
				flags[j] = true
			}
		}
	}
	return count
}

func main() {
	println("nsieve", size, sieve(size), sieve(size/2))
}
