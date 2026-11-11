package main

import "math"

func main() {
	for _, v := range []float64{0, 1, -1, 0.1, 1e300, 5e-324, math.Pi} {
		b := math.Float64bits(v)
		println(b, math.Float64frombits(b) == v)
	}
	for _, v := range []float32{0, 1, -1, 0.1, 1e30} {
		b := math.Float32bits(v)
		println(b, math.Float32frombits(b) == v)
	}
	fr, ex := math.Frexp(12.5)
	println(fr, ex, math.Ldexp(fr, ex))
	println(math.Float32frombits(0x7f800000), math.Float64frombits(0xfff0000000000000))
	println(math.Ilogb(8), math.Logb(10), math.Gamma(5), math.Erf(0))
}
