// matmul: dense float64 matrix multiply on slices of slices.
package main

const (
	n    = 800
	reps = 20
)

func main() {
	a := make([][]float64, n)
	b := make([][]float64, n)
	c := make([][]float64, n)
	for i := 0; i < n; i++ {
		a[i] = make([]float64, n)
		b[i] = make([]float64, n)
		c[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			a[i][j] = float64((i*j)%7) + 0.5
			b[i][j] = float64((i+j)%5) - 1.5
		}
	}
	for r := 0; r < reps; r++ {
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				s := 0.0
				for k := 0; k < n; k++ {
					s += a[i][k] * b[k][j]
				}
				c[i][j] = s
			}
		}
	}
	tr := 0.0
	for i := 0; i < n; i++ {
		tr += c[i][i]
	}
	println("matmul", int64(tr*100))
}
