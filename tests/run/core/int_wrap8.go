package main

func main() {
	var a int8 = 127
	a++
	println(a)
	var b int8 = -128
	b--
	println(b)
	var c uint8 = 255
	c++
	println(c)
	var d uint8
	d--
	println(d)
	var e int8 = -128
	println(-e)
	println(e * -1)
	var f int8 = 100
	println(f + f)
	var g uint8 = 200
	println(g * 2)
}
