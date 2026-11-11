package main

import "math"

func main() {
	println(math.MaxInt8, math.MinInt8, math.MaxInt16, math.MinInt16, math.MaxInt32, math.MinInt32)
	println(math.MaxInt64, math.MinInt64, math.MaxUint8, math.MaxUint16, math.MaxUint32)
	var u uint64 = math.MaxUint64
	println(u, math.MaxInt, math.MinInt)
	println(math.MaxFloat32, math.SmallestNonzeroFloat32, math.MaxFloat64, math.SmallestNonzeroFloat64)
	println(math.Pi, math.E, math.Sqrt2, math.Ln2, math.Phi, math.SqrtE)
	println(float32(math.Pi), float32(math.MaxFloat32))
}
