// transpose: naive out of place matrix transpose of float64.
package main

const (
	n    = 2048
	reps = 40
)

func main() {
	a := make([]float64, n*n)
	b := make([]float64, n*n)
	for i := range a {
		a[i] = float64(i % 1024)
	}
	for r := 0; r < reps; r++ {
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				b[j*n+i] = a[i*n+j]
			}
		}
		a, b = b, a
	}
	s := 0.0
	for i := 0; i < n; i++ {
		s += a[i*n+i] * float64(i%7)
	}
	println("transpose", int64(s))
}
