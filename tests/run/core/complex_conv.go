package main

func main() {
	var a complex64 = complex(1.25, 2.5)
	b := complex128(a)
	println(real(b), imag(b))
	c := complex64(complex(1.1, 2.2))
	println(real(c), imag(c))
	var z complex128
	println(z == 0, z)
	z += 1i
	println(imag(z))
	x := 1 + 2i
	x *= x
	println(real(x), imag(x))
}
