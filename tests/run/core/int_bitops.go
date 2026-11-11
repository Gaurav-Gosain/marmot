package main

func main() {
	var a, b uint8 = 0xf0, 0x3c
	println(a&b, a|b, a^b, a&^b)
	println(^a, ^b)
	var c, d int16 = -2, 0x0f0f
	println(c&d, c|d, c^d, c&^d)
	println(^c, ^d)
	var e int64 = -1
	println(^e, e&^e, e^e)
	var f uint = 0
	println(^f)
	const k = ^0
	println(k)
	var g uint32 = 0xdeadbeef
	println(g, g>>16, g&0xffff)
}
