package main

func main() {
	var a int16 = 32767
	a += 1
	println(a)
	var b uint16 = 0
	b -= 1
	println(b)
	var c int32 = 2147483647
	c++
	println(c)
	var d uint32 = 4294967295
	d += 2
	println(d)
	var e int32 = -2147483648
	println(e / -1)
	println(e * e)
	var f uint32 = 65536
	println(f * f)
}
