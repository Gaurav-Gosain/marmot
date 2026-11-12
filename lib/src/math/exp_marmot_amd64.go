// marmot overlay: gc's math/exp_amd64.s (archExp) in Go, bit identical to
// gc on amd64. gc picks the FMA variant when the CPU has AVX and FMA; the
// runtime core's x86-64 level 3 (AVX2, FMA) selects it here. Operations
// and their order follow the assembly: FMA for the fused instructions,
// explicit float64 conversions around separate products, CVTSD2SL
// rounding (nearest even, out of range gives the integer indefinite) and
// the assembly's own ldexp.

package math

import _ "unsafe" // for linkname

const haveArchExp = true

//go:linkname rt_x86_level rt_x86_level
func rt_x86_level() int32

var useFMA = rt_x86_level() >= 3

const (
	archExpLN2U     = 0.69314718055966295651160180568695068359375
	archExpLN2L     = 0.28235290563031577122588448175013436025525412068e-12
	archExpLOG2E    = 1.4426950408889634073599246810018920
	archExpOverflow = 7.09782712893384e+02
)

// cvtsd2sl is CVTSD2SL: round to nearest even, and 0x80000000 for NaN or
// values out of the int32 range.
func cvtsd2sl(x float64) int32 {
	r := RoundToEven(x)
	if !(r >= -2147483648 && r <= 2147483647) {
		return -2147483648
	}
	return int32(r)
}

func archExp(x float64) float64 {
	bits := Float64bits(x)
	if bits&^(1<<63) >= 0x7FF0000000000000 {
		if bits == 0xFFF0000000000000 {
			return 0 // -Inf
		}
		return x // NaN or +Inf
	}
	if x > archExpOverflow {
		return Inf(1)
	}
	e := cvtsd2sl(float64(archExpLOG2E * x))
	fe := float64(e)
	var y float64
	if useFMA {
		x = FMA(-fe, archExpLN2U, x)
		x = FMA(-fe, archExpLN2L, x)
		x = float64(x * 0.0625)
		t := FMA(2.4801587301587301587e-5, x, 1.9841269841269841270e-4)
		t = FMA(t, x, 1.3888888888888888889e-3)
		t = FMA(t, x, 8.3333333333333333333e-3)
		t = FMA(t, x, 4.1666666666666666667e-2)
		t = FMA(t, x, 1.6666666666666666667e-1)
		t = FMA(t, x, 0.5)
		t = FMA(t, x, 1.0)
		x = float64(x * t)
		for i := 0; i < 3; i++ {
			x = float64(x * (x + 2.0))
		}
		y = FMA(x, x+2.0, 1.0)
	} else {
		x = x - float64(fe*archExpLN2U)
		x = x - float64(fe*archExpLN2L)
		x = float64(x * 0.0625)
		t := float64(2.4801587301587301587e-5*x) + 1.9841269841269841270e-4
		t = float64(t*x) + 1.3888888888888888889e-3
		t = float64(t*x) + 8.3333333333333333333e-3
		t = float64(t*x) + 4.1666666666666666667e-2
		t = float64(t*x) + 1.6666666666666666667e-1
		t = float64(t*x) + 0.5
		t = float64(t*x) + 1.0
		x = float64(x * t)
		for i := 0; i < 4; i++ {
			x = float64(x * (x + 2.0))
		}
		y = x + 1.0
	}
	// ldexp as in the assembly (32-bit exponent arithmetic).
	b := e + 0x3FF
	if b <= 0 {
		if b < -52 {
			return 0
		}
		y = y * Float64frombits(uint64(uint32(b+0x3FE))<<52)
		b = 1
	} else if b >= 0x7FF {
		return Inf(1)
	}
	return y * Float64frombits(uint64(uint32(b))<<52)
}
