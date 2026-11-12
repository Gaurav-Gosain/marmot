// marmot overlay: gc's math/hypot_amd64.s (archHypot) in Go, bit identical
// to gc on amd64: max * sqrt(1 + (min/max)**2) with every operation
// rounded on its own (no FMA for 1 + q*q).

package math

const haveArchHypot = true

func archHypot(p, q float64) float64 {
	pb := Float64bits(p) &^ (1 << 63)
	qb := Float64bits(q) &^ (1 << 63)
	const posInf = 0x7FF0000000000000
	if pb >= posInf || qb >= posInf {
		if pb == posInf || qb == posInf {
			return Inf(1)
		}
		return Float64frombits(0x7FF8000000000001)
	}
	if pb|qb == 0 {
		return 0
	}
	p, q = Float64frombits(pb), Float64frombits(qb)
	hi, lo := p, q
	if q > p {
		hi, lo = q, p
	}
	r := lo / hi
	r = float64(r*r) + 1
	return hi * Sqrt(r)
}
