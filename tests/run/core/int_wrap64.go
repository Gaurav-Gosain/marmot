package main

func main() {
	var a int64 = 9223372036854775807
	a++
	println(a)
	var b uint64 = 18446744073709551615
	b++
	println(b)
	var c int64 = -9223372036854775808
	println(-c)
	println(c / -1)
	println(c % -1)
	var d int = 1 << 62
	println(d * 4)
	println(d * 2)
	var e uint64 = 1 << 63
	println(e * 2)
	println(e + e - 1)
}
