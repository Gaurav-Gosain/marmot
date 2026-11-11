package main

// The Go spec allows fusing x*y+z into one operation. gc does this on arm64 and the result can
// differ in the last bit from separately rounded arithmetic. This test records what stock Go
// prints for the unfused-looking source so a difference is visible. Expected to differ between
// compilers: see tests/known_failures.txt.
func dot(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func main() {
	x := make([]float64, 777)
	y := make([]float64, 777)
	for i := range x {
		x[i] = float64(i)*0.001 + 0.0003
		y[i] = 1e8 / float64(i+3)
	}
	println(dot(x, y))
	a, b, c := 1.0000001, 3.0000003, -3.0000006
	println(a*b + c)
	println(float64(a*b) + c)
}
