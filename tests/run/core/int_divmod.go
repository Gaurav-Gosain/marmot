package main

func main() {
	println(7/2, -7/2, 7/-2, -7/-2)
	println(7%2, -7%2, 7%-2, -7%-2)
	var a, b int8 = -128, -1
	println(a/b, a%b)
	var c, d int32 = 100, 7
	println(c/d, c%d, -c/d, -c%d)
	var e, f uint8 = 255, 16
	println(e/f, e%f)
	var g, h int64 = -9, 4
	println(g/h, g%h)
	var x, y int = 1 << 40, 1000
	println(x/y, x%y)
}
