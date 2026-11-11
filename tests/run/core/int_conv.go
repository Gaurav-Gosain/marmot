package main

func main() {
	var a int = 300
	println(int8(a), uint8(a), int16(a))
	var b int = -1
	println(uint8(b), uint16(b), uint32(b), uint64(b))
	var c int8 = -1
	println(uint8(c), uint16(c), uint32(c), uint64(c), int64(c))
	var d uint8 = 200
	println(int8(d), int16(d), int64(d))
	var e int64 = 1<<40 + 5
	println(int32(e), uint32(e), int16(e))
	var f uint64 = 1<<63 + 1
	println(int64(f), int32(f))
	var g uint32 = 1 << 31
	println(int32(g), int64(g), int64(int32(g)))
}
