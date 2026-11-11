package main

import "math"

func main() {
	for _, v := range []float64{0, 0.5, 1, math.Pi / 2, math.Pi, -1, 10, 1e6} {
		println(math.Sin(v), math.Cos(v), math.Tan(v))
	}
	println(math.Asin(1), math.Acos(0), math.Atan(1), math.Atan2(-1, -1), math.Atan2(0, -1))
	println(math.Sinh(1), math.Cosh(1), math.Tanh(1), math.Asinh(1), math.Acosh(2), math.Atanh(0.5))
	s, c := math.Sincos(1)
	println(s, c)
}
