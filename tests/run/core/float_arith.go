package main

func main() {
	a, b := 7.5, 2.0
	println(a+b, a-b, a*b, a/b)
	var c, d float32 = 1.1, 2.2
	println(c+d, c*d, d/c)
	x := 1.0
	for i := 0; i < 10; i++ {
		x *= 1.5
	}
	println(x)
	var s float64
	for i := 1; i <= 1000; i++ {
		s += 1 / float64(i)
	}
	println(s)
	println(-a, +a)
	e := 5.0
	e += 1
	e -= 0.5
	e *= 2
	e /= 4
	println(e)
}
