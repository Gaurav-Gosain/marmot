package main

//go:noinline
func id(n uint) uint { return n }

func main() {
	var one int = 1
	println(one<<id(63), one<<id(64), one<<id(200))
	var u uint32 = 1
	println(u<<id(31), u<<id(32), u<<id(33))
	var n int32 = -8
	println(n>>id(1), n>>id(31), n>>id(32), n>>id(100))
	var m uint32 = 0x80000000
	println(m>>id(31), m>>id(32), m>>id(100))
	var i8 int8 = -128
	println(i8>>id(7), i8>>id(8), i8>>id(1))
	var u8 uint8 = 0xff
	println(u8<<id(4), u8>>id(4), u8<<id(8), u8>>id(8))
	var sh int = 3
	println(1<<sh, 1000>>sh)
	var big uint64 = 1 << 63
	println(big>>id(63), big>>id(64), big<<id(1))
	var sc uint8 = 70
	println(int64(1)<<sc, uint64(1)<<(sc-7))
}
