package main

import "math"

func main() {
	nan, inf := math.NaN(), math.Inf(1)
	println(math.IsNaN(nan), math.IsNaN(1), math.IsInf(inf, 1), math.IsInf(inf, -1), math.IsInf(-inf, 0), math.IsInf(1, 0))
	println(math.Max(1, nan), math.Min(nan, 1), math.Max(-0.0, 0), math.Min(0, math.Copysign(0, -1)))
	println(math.Abs(-0.0), math.Abs(math.Inf(-1)), math.Abs(-5.5))
	println(math.Mod(7, -3), math.Mod(-7, 3), math.Mod(5.5, 2), math.Mod(1, 0), math.Mod(inf, 1))
	println(math.Remainder(7, 3), math.Remainder(8, 3), math.Dim(5, 3), math.Dim(3, 5))
	println(math.Signbit(-1), math.Signbit(0), math.Signbit(math.Copysign(0, -1)))
	println(math.Float64bits(math.Inf(1)), math.Float64bits(-0.0) == 0, math.Float32bits(float32(nan)) != 0)
	println(math.Nextafter(1, 2), math.Nextafter(1, 0), math.Nextafter32(1, 2))
}
