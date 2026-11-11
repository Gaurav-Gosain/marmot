package main

func main() {
	println(0, -0, 1, -1)
	println(int8(-128), int8(127), uint8(255))
	println(int16(-32768), uint16(65535))
	println(int32(-2147483648), uint32(4294967295))
	println(int64(-9223372036854775808), uint64(18446744073709551615))
	println(uintptr(12345))
	println(0x7fffffffffffffff, -0x8000000000000000)
	println('a', 'é', '世', '\n')
	println(1, 2, 3)
	print("x", 1, true, "\n")
	print()
	println()
}
