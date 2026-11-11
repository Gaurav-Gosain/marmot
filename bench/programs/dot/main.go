// dot: integer dot product of two vectors that fit in cache, repeated.
package main

const (
	n    = 1 << 14
	reps = 1200000
)

func dot(a, b []int64) int64 {
	var s int64
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func main() {
	a := make([]int64, n)
	b := make([]int64, n)
	for i := range a {
		a[i] = int64(i%97) - 40
		b[i] = int64(i%31) - 9
	}
	var total int64
	for r := 0; r < reps; r++ {
		a[r&(n-1)]++
		total += dot(a, b)
	}
	println("dot", total)
}
