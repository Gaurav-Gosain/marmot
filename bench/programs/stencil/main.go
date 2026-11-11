// stencil: 5-point 2D stencil on a float64 grid. Weights are exact binary fractions.
package main

const (
	size  = 512
	iters = 1000
)

func main() {
	a := make([]float64, size*size)
	b := make([]float64, size*size)
	for i := range a {
		a[i] = float64(i%8) / 4
	}
	for it := 0; it < iters; it++ {
		for y := 1; y < size-1; y++ {
			for x := 1; x < size-1; x++ {
				i := y*size + x
				b[i] = 0.5*a[i] + 0.125*(a[i-1]+a[i+1]+a[i-size]+a[i+size])
			}
		}
		a, b = b, a
	}
	s := 0.0
	for _, v := range a {
		s += v
	}
	println("stencil", int64(s*1024))
}
