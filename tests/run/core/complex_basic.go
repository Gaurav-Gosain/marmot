package main

func main() {
	a := complex(1, 2)
	b := complex(3, -1)
	println(real(a), imag(a))
	c := a + b
	println(real(c), imag(c))
	d := a * b
	println(real(d), imag(d))
	e := a / b
	println(real(e), imag(e))
	println(a == b, a == complex(1, 2), a != b)
	var f complex64 = complex(1.5, -2.5)
	println(real(f), imag(f))
	println(-a)
	g := a - b
	println(real(g), imag(g))
	const k = 2i * 2i
	println(real(k), imag(k))
	println(a)
}
