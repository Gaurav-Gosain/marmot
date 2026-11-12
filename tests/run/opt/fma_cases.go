package main

import "math"

// NaN inputs are left out on purpose: the sign and payload of a NaN result are not defined by the
// Go spec and differ between compilers and architectures (see docs/support-matrix.md, known differences).
//
// Cases where gc on arm64 fuses (or must not fuse) multiply-add. The expected output is
// whatever stock Go prints; marmot must match it bit for bit.
//
//go:noinline
func mulAdd(x, y, z float64) float64 { return x*y + z }

//go:noinline
func mulSub(x, y, z float64) float64 { return x*y - z }

//go:noinline
func subMul(x, y, z float64) float64 { return z - x*y }

//go:noinline
func explicit(x, y, z float64) float64 { return float64(x*y) + z }

//go:noinline
func twoUses(x, y, z float64) (float64, float64) {
	p := x * y
	return p + z, p
}

//go:noinline
func mulAdd32(x, y, z float32) float32 { return x*y + z }

//go:noinline
func explicit32(x, y, z float32) float32 { return float32(x*y) + z }

//go:noinline
func negMul(x, y, z float64) float64 { return -(x * y) + z }

//go:noinline
func chain(a []float64, x float64) float64 {
	r := 0.0
	for _, c := range a {
		r = r*x + c
	}
	return r
}

//go:noinline
func accum(a, b []float64) (s float64) {
	for i := range a {
		s += a[i] * b[i]
	}
	return
}

func bits(f float64) uint64 { return math.Float64bits(f) }

func main() {
	vals := [][3]float64{
		{1.0000001, 3.0000003, -3.0000006},
		{0.1, 0.1, -0.01},
		{1e8 + 0.5, 1e8 - 0.5, -1e16},
		{3, 7, 1},
		{math.Inf(1), 0, 1},
		{1e308, 10, -1e308},
		{0.7, 0.3, -0.21},
		{-0.0, 1, -0.0},
	}
	for _, v := range vals {
		x, y, z := v[0], v[1], v[2]
		p1, p2 := twoUses(x, y, z)
		println(bits(mulAdd(x, y, z)), bits(mulSub(x, y, z)), bits(subMul(x, y, z)), bits(explicit(x, y, z)), bits(p1), bits(p2), bits(negMul(x, y, z)))
		println(mulAdd32(float32(x), float32(y), float32(z)), explicit32(float32(x), float32(y), float32(z)))
	}
	a := make([]float64, 200)
	b := make([]float64, 200)
	for i := range a {
		a[i] = float64(i)*0.37 - 11
		b[i] = 1/float64(i+1) + 0.001
	}
	println(bits(chain(a, 1.0001)), bits(chain(a, 0.9)), bits(accum(a, b)))
	var f32 float32
	for i := 0; i < 1000; i++ {
		f32 = f32*0.999 + float32(i)*0.01
	}
	println(f32)
	c := complex(1.0000001, 2.0000003)
	d := complex(3.0000003, -1.0000001)
	println(real(c*d), imag(c*d))
}
