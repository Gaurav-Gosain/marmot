// saxpy: y = a*x + y over float64 vectors. Values are exact binary fractions, so the result does
// not depend on fused multiply-add.
package main

const (
	n    = 1 << 14
	reps = 100000
)

func saxpy(a float64, x, y []float64) {
	for i := range x {
		y[i] += a * x[i]
	}
}

func main() {
	x := make([]float64, n)
	y := make([]float64, n)
	for i := range x {
		x[i] = float64(i%16) / 8
	}
	for r := 0; r < reps; r++ {
		saxpy(0.5, x, y)
		if r%1024 == 1023 {
			for i := range y {
				y[i] = 0
			}
		}
	}
	s := 0.0
	for _, v := range y {
		s += v
	}
	println("saxpy", int64(s*8))
}
