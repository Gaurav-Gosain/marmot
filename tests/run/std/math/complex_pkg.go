package main

import "math/cmplx"

func main() {
	z := complex(3, 4)
	println(cmplx.Abs(z), real(cmplx.Conj(z)), imag(cmplx.Conj(z)))
	s := cmplx.Sqrt(-1)
	println(real(s), imag(s))
	e := cmplx.Exp(complex(0, 3.141592653589793))
	println(real(e), imag(e) < 1e-15)
	println(cmplx.Phase(complex(0, 1)), cmplx.IsNaN(cmplx.NaN()), cmplx.IsInf(cmplx.Inf()))
	r, th := cmplx.Polar(complex(0, 2))
	println(r, th)
	p := cmplx.Pow(complex(0, 1), 2)
	println(real(p), imag(p) < 1e-15)
}
