// prefixsum: inclusive prefix sum over int64, repeated.
package main

const (
	n    = 1 << 16
	reps = 75000
)

func main() {
	a := make([]int64, n)
	for i := range a {
		a[i] = int64(i%13) - 5
	}
	var total int64
	for r := 0; r < reps; r++ {
		a[0] = int64(r & 7)
		for i := 1; i < len(a); i++ {
			a[i] += a[i-1]
		}
		total += a[n-1]
		for i := range a {
			a[i] = int64(i%13) - 5
		}
	}
	println("prefixsum", total)
}
