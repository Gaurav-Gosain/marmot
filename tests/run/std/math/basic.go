package main

import "math"

func main() {
	println(math.Sqrt(2), math.Pow(2, 10), math.Abs(-3.5), math.Floor(-1.5), math.Ceil(1.2), math.Trunc(-2.7), math.Round(2.5), math.Round(-2.5))
	println(math.Max(1, 2), math.Min(1, 2), math.Mod(7, 3), math.Hypot(3, 4))
	println(math.Inf(1), math.Inf(-1), math.IsNaN(math.NaN()), math.IsInf(math.Inf(1), 0))
	println(math.MaxInt64, math.MinInt64, math.MaxUint32, math.MaxInt8)
	println(math.Pi, math.E)
	println(math.Float64bits(1.0), math.Float32bits(1.0), math.Float64frombits(0x4000000000000000))
	println(math.Signbit(-0.0), math.Signbit(math.Copysign(0, -1)), math.Copysign(3, -1))
	println(math.Exp(1), math.Log(math.E), math.Log2(8), math.Log10(1000))
	println(math.Sin(0), math.Cos(0), math.Atan2(1, 1))
	fr, ex := math.Frexp(8)
	println(fr, ex, math.Ldexp(0.5, 4))
	println(math.Cbrt(27), math.MaxFloat64, math.SmallestNonzeroFloat64)
}
