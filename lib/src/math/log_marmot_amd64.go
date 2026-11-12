// marmot overlay: gc's math/log_amd64.s (archLog) in Go, bit identical to
// gc on amd64. It follows the assembly, which differs from log.go: Frexp
// by bit fields without normalizing subnormals, the f1 <= Sqrt2/2 test
// (CMPSD not-less-than), and every product rounded on its own (no FMA).

package math

const haveArchLog = true

func archLog(x float64) float64 {
	const (
		HSqrt2 = 7.07106781186547524401e-01
		Ln2Hi  = 6.93147180369123816490e-01
		Ln2Lo  = 1.90821492927058770002e-10
		L1     = 6.666666666666735130e-01
		L2     = 3.999999999940941908e-01
		L3     = 2.857142874366239149e-01
		L4     = 2.222219843214978396e-01
		L5     = 1.818357216161805012e-01
		L6     = 1.531383769920937332e-01
		L7     = 1.479819860511658591e-01
	)
	bits := Float64bits(x)
	switch {
	case bits&^(1<<63) == 0:
		return Inf(-1)
	case int64(bits) < 0:
		return Float64frombits(0x7FF8000000000001)
	case bits >= 0x7FF0000000000000:
		return x // +Inf or NaN
	}
	f1 := Float64frombits(bits&0x000FFFFFFFFFFFFF | 0x3FE0000000000000)
	k := float64(int32(bits>>52&0x7FF) - 0x3FE)
	if !(HSqrt2 < f1) {
		k -= 1
		f1 = float64(f1 * 2)
	}
	f := f1 - 1
	s := f / (2 + f)
	s2 := float64(s * s)
	s4 := float64(s2 * s2)
	t1 := float64(L7*s4) + L5
	t1 = float64(t1*s4) + L3
	t1 = float64(t1*s4) + L1
	t1 = float64(s2 * t1)
	t2 := float64(L6*s4) + L4
	t2 = float64(t2*s4) + L2
	t2 = float64(s4 * t2)
	R := t1 + t2
	hfsq := float64(float64(0.5*f) * f)
	a := float64(s*(R+hfsq)) + float64(Ln2Lo*k)
	b := (hfsq - a) - f
	return float64(k*Ln2Hi) - b
}
