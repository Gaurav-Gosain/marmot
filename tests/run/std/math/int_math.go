package main

import "math"

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func main() {
	println(abs(-5), abs(math.MinInt64), abs(math.MaxInt64))
	var x int32 = math.MaxInt32
	x++
	println(x)
	m32, m8 := uint32(math.MaxUint32), uint8(math.MaxUint8)
	println(m32+1, m8+1)
	println(int(math.Floor(2.9)), int(math.Ceil(2.1)), int64(math.Round(-0.5)))
	println(math.MaxInt64/2, math.MinInt64/2)
}
