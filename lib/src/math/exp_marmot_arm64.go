// marmot overlay: gc's math/exp_arm64.s (archExp and archExp2) in Go. The
// operations and their order follow the assembly: FMA where it uses fused
// multiply-add instructions (FMADDD, FMSUBD, FNMSUBD), and explicit
// float64 conversions around separate products so that the compiler does
// not fuse them. Results are bit identical to gc on arm64, including the
// inline Ldexp that flushes some tiny results to zero.

package math

const (
	haveArchExp  = true
	haveArchExp2 = true
)

const (
	archExpLn2Hi      = 6.93147180369123816490e-01
	archExpLn2Lo      = 1.90821492927058770002e-10
	archExpLog2e      = 1.44269504088896338700e+00
	archExpOverflow   = 7.09782712893383973096e+02
	archExpUnderflow  = -7.45133219101941108420e+02
	archExpOverflow2  = 1.0239999999999999e+03
	archExpUnderflow2 = -1.0740e+03
	archExpNearZero   = 0x1p-28
	archExpP1         = 1.66666666666666657415e-01
	archExpP2         = -2.77777777770155933842e-03
	archExpP3         = 6.61375632143793436117e-05
	archExpP4         = -1.65339022054652515390e-06
	archExpP5         = 4.13813679705723846039e-08
	archExpFracMask   = 0x000fffffffffffff
)

// archExpTail finishes both functions from the reduced argument: r = hi -
// lo, k the power of two. It is the shared tail of the two assembly
// routines (polynomial, y, inline Ldexp).
func archExpTail(hi, lo float64, k int64) float64 {
	r := hi - lo
	t := float64(r * r)
	c := FMA(t, archExpP5, archExpP4)
	c = FMA(t, c, archExpP3)
	c = FMA(t, c, archExpP2)
	c = FMA(t, c, archExpP1)
	c = FMA(-t, c, r) // FMSUBD: r - t*c
	q := float64(r*c) / (2 - c)
	y := 1 - ((lo - q) - hi)
	// Inline Ldexp(y, k), as the assembly: no checks for Inf, NaN or
	// zero, and a subnormal result scaled by 2**-52 after the fact.
	bits := Float64bits(y)
	frac := bits & archExpFracMask
	e := int64(bits>>52) + k
	m := 1.0
	if e < 1 {
		e += 52
		m = 0x1p-52
	}
	return Float64frombits(uint64(e)<<52|frac) * m
}

func archExp(x float64) float64 {
	switch {
	case x != x:
		return x
	case x > archExpOverflow:
		return Inf(1)
	case x < archExpUnderflow:
		return 0
	case Abs(x) < archExpNearZero:
		return 1 + x
	}
	// k = int(Log2e*x -+ 0.5), the products fused (FNMSUBD, FMADDD).
	var kf float64
	if x < 0 {
		kf = FMA(archExpLog2e, x, -0.5)
	} else {
		kf = FMA(archExpLog2e, x, 0.5)
	}
	k := int64(kf)
	fk := float64(k)
	hi := FMA(-fk, archExpLn2Hi, x) // FMSUBD: x - k*Ln2Hi
	lo := float64(fk * archExpLn2Lo)
	return archExpTail(hi, lo, k)
}

func archExp2(x float64) float64 {
	switch {
	case x != x:
		return x
	case x > archExpOverflow2:
		return Inf(1)
	case x < archExpUnderflow2:
		return 0
	}
	var kf float64
	if x < 0 {
		kf = x - 0.5
	} else {
		kf = x + 0.5
	}
	k := int64(kf)
	t := x - float64(k)
	hi := float64(t * archExpLn2Hi)
	lo := -float64(t * archExpLn2Lo) // FNMULD
	return archExpTail(hi, lo, k)
}
