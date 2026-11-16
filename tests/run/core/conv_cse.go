// Conversions of one value to several types in one function: each keeps its
// own result. tier0's MIR once shared them by operation and operand, so
// float32(x) took float64(x)'s result.
package main

func fromInt(x int64) (float32, float64) { return float32(x), float64(x) }

func fromUint(x uint32) (float32, float64) { return float32(x), float64(x) }

func fromFloat(x float64) (float32, float64) { return float32(x), float64(x) }

func widths(x int64) (int8, uint8, int16, uint16, int32, uint32, uint64) {
	return int8(x), uint8(x), int16(x), uint16(x), int32(x), uint32(x), uint64(x)
}

func fromSmall(x int8) (int64, uint64, uint8, float32, float64) {
	return int64(x), uint64(x), uint8(x), float32(x), float64(x)
}

func toInts(x float64) (int32, int64, uint8, uint32) { return int32(x), int64(x), uint8(x), uint32(x) }

func main() {
	a, b := fromInt(1<<53 + 1)
	println(a, b)
	c, d := fromUint(4294967295)
	println(c, d)
	e, f := fromFloat(0.1)
	println(e, f)
	i8, u8, i16, u16, i32, u32, u64 := widths(-1234567890123)
	println(i8, u8, i16, u16, i32, u32, u64)
	s1, s2, s3, s4, s5 := fromSmall(-7)
	println(s1, s2, s3, s4, s5)
	t1, t2, t3, t4 := toInts(200.75)
	println(t1, t2, t3, t4)
}
