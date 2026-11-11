// Complex division and float helpers. Pure file.

package runtime

import "unsafe"

var inf = float64frombits(0x7FF0000000000000)

func float64bits(f float64) uint64     { return *(*uint64)(unsafe.Pointer(&f)) }
func float64frombits(b uint64) float64 { return *(*float64)(unsafe.Pointer(&b)) }

func isNaN(f float64) bool    { return f != f }
func isFinite(f float64) bool { return !isNaN(f - f) }
func isInf(f float64) bool    { return !isNaN(f) && !isFinite(f) }

func abs(x float64) float64 {
	const sign = 1 << 63
	return float64frombits(float64bits(x) &^ sign)
}

func copysign(x, y float64) float64 {
	const sign = 1 << 63
	return float64frombits(float64bits(x)&^sign | float64bits(y)&sign)
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// complex128div implements n / m with the gc runtime's results for
// infinities, NaNs and zeros (C99 annex G).
func complex128div(n complex128, m complex128) complex128 {
	var e, f float64 // complex(e, f) = n/m

	// Algorithm for robust complex division as described in
	// Robert L. Smith: Algorithm 116: Complex division. Commun. ACM 5(8): 435 (1962).
	if abs(real(m)) >= abs(imag(m)) {
		ratio := imag(m) / real(m)
		denom := real(m) + ratio*imag(m)
		e = (real(n) + imag(n)*ratio) / denom
		f = (imag(n) - real(n)*ratio) / denom
	} else {
		ratio := real(m) / imag(m)
		denom := imag(m) + ratio*real(m)
		e = (real(n)*ratio + imag(n)) / denom
		f = (imag(n)*ratio - real(n)) / denom
	}

	if isNaN(e) && isNaN(f) {
		// Correct final result to infinities and zeros if applicable.
		// Matches C99: ISO/IEC 9899:1999 - G.5.1  Multiplicative operators.
		a, b := real(n), imag(n)
		c, d := real(m), imag(m)

		switch {
		case m == 0 && (!isNaN(a) || !isNaN(b)):
			e = copysign(inf, c) * a
			f = copysign(inf, c) * b

		case (isInf(a) || isInf(b)) && isFinite(c) && isFinite(d):
			a = copysign(boolToFloat(isInf(a)), a)
			b = copysign(boolToFloat(isInf(b)), b)
			e = inf * (a*c + b*d)
			f = inf * (b*c - a*d)

		case (isInf(c) || isInf(d)) && isFinite(a) && isFinite(b):
			c = copysign(boolToFloat(isInf(c)), c)
			d = copysign(boolToFloat(isInf(d)), d)
			e = 0 * (a*c + b*d)
			f = 0 * (b*c - a*d)
		}
	}

	return complex(e, f)
}
