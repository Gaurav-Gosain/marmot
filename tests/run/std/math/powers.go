package main

import "math"

func main() {
	println(math.Sqrt(0), math.Sqrt(2), math.Sqrt(-1), math.Sqrt(math.Inf(1)), math.Cbrt(-8))
	println(math.Pow(2, 0.5), math.Pow(0, 0), math.Pow(-8, 1.0/3), math.Pow(2, -2), math.Pow(10, 308), math.Pow(10, 309))
	println(math.Pow(-2, 3), math.Pow(-2, 2), math.Pow(0, -1), math.Pow(1, math.NaN()))
	println(math.Exp(0), math.Exp(1), math.Exp(-1000), math.Exp(1000), math.Exp2(10), math.Expm1(1e-10))
	println(math.Log(1), math.Log(0), math.Log(-1), math.Log2(1024), math.Log10(0.001), math.Log1p(1e-10))
	println(math.Hypot(3, 4), math.Hypot(math.Inf(1), math.NaN()))
}
