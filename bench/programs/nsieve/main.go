// nsieve: sieve of Eratosthenes on a byte slice, bounds checks in the hot loop.
package main

const (
	size = 150000000
	reps = 4
)

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
	a, b := 0, 0
	for r := 0; r < reps; r++ {
		a += sieve(size)
		b += sieve(size/2 + r)
	}
	println("nsieve", size, a, b)
}
