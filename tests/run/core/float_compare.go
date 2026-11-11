package main

func main() {
	a, b := 1.0, 2.0
	println(a < b, a <= b, a > b, a >= b, a == b, a != b)
	var z float64
	println(z == -z, 1/z == 1/(-z))
	var f, g float32 = 0.1, 0.1
	println(f == g, float64(f) == 0.1)
	println(0.1+0.2 == 0.3)
	x, y := 0.1, 0.2
	println(x+y == 0.3)
}
