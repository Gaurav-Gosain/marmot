package main

func main() {
	println(0.0, 1.0, -1.0, 0.5, 1.5, 100.0)
	println(3.14159, 1e10, 1e-10, 1e100, 1e-100)
	println(float32(0.1), float32(16777216.0), float32(1.5))
	var a float64 = 123456789.123456789
	println(a)
	var b float32 = 123456789.0
	println(b)
	println(float64(float32(0.1)))
	println(0.1+0.2, 1.0/3.0)
}
