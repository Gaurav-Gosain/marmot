package main

func main() {
	var a int8 = -1
	var b uint8 = 255
	println(a < 0, b > 0, uint8(a) == b)
	var c int64 = -1
	var d uint64 = uint64(c)
	println(c < 0, d > 0, d == 1<<64-1)
	println(int32(-5) < int32(3), uint32(1<<31) > uint32(1))
	var e, f int = 5, 5
	println(e == f, e != f, e <= f, e >= f, e < f, e > f)
	var g int16 = -32768
	println(g < 0, -g == g)
}
