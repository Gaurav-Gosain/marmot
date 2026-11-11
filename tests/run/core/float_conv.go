package main

func main() {
	f := 3.99
	println(int(f), int(-f), int8(f), uint8(f))
	g := 1e10
	println(int64(g), uint64(g))
	var h float32 = 16777217
	println(h, int(h))
	var i int64 = 1<<53 + 1
	println(float64(i), float32(i))
	var u uint64 = 1<<64 - 1
	println(float64(u))
	var j uint32 = 4294967295
	println(float32(j), float64(j))
	println(float64(int8(-5)), float32(uint8(250)))
	var k float64 = -0.5
	println(int(k), uint8(int(k)+1))
}
