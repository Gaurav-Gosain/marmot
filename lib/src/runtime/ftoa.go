// Shortest decimal formatting of floats for print, println and panic
// values, without strconv's lookup tables (binary size). The output is
// identical to strconv.FormatFloat(v, 'g', -1, bitSize): the digits come
// from an exact multiprecision decimal conversion followed by the shortest
// rounding that still reads back as the same float (the classic strconv
// algorithm before Ryu). Pure file.
//
// Adapted from the Go project's strconv (BSD license).

package runtime

import "unsafe"

// fdecimal is a multiprecision decimal number: 0.d[0:nd] * 10^dp.
type fdecimal struct {
	d     [800]byte // digits, big-endian representation
	nd    int       // number of digits used
	dp    int       // decimal point
	trunc bool      // discarded nonzero digits beyond d[:nd]
}

func (a *fdecimal) trim() {
	for a.nd > 0 && a.d[a.nd-1] == '0' {
		a.nd--
	}
	if a.nd == 0 {
		a.dp = 0
	}
}

// assign sets a to v.
func (a *fdecimal) assign(v uint64) {
	var buf [24]byte
	n := 0
	for v > 0 {
		v1 := v / 10
		v -= 10 * v1
		buf[n] = byte(v + '0')
		n++
		v = v1
	}
	a.nd = 0
	for n--; n >= 0; n-- {
		a.d[a.nd] = buf[n]
		a.nd++
	}
	a.dp = a.nd
	a.trim()
}

const fmaxShift = 60

// rightShift divides a by 2^k (k <= fmaxShift).
func (a *fdecimal) rightShift(k uint) {
	r := 0 // read pointer
	w := 0 // write pointer

	// Pick up enough leading digits to cover the first shift.
	var n uint64
	for ; n>>k == 0; r++ {
		if r >= a.nd {
			if n == 0 {
				a.nd = 0
				return
			}
			for n>>k == 0 {
				n = n * 10
				r++
			}
			break
		}
		c := uint64(a.d[r])
		n = n*10 + c - '0'
	}
	a.dp -= r - 1

	mask := uint64(1)<<k - 1

	// Pick up a digit, put down a digit.
	for ; r < a.nd; r++ {
		c := uint64(a.d[r])
		dig := n >> k
		n &= mask
		a.d[w] = byte(dig + '0')
		w++
		n = n*10 + c - '0'
	}

	// Put down extra digits.
	for n > 0 {
		dig := n >> k
		n &= mask
		if w < len(a.d) {
			a.d[w] = byte(dig + '0')
			w++
		} else if dig > 0 {
			a.trunc = true
		}
		n = n * 10
	}

	a.nd = w
	a.trim()
}

// leftShift multiplies a by 2^k (k <= fmaxShift), digit by digit from the
// least significant end.
func (a *fdecimal) leftShift(k uint) {
	var buf [len(fdecimal{}.d) + 24]byte
	w := len(buf)
	var carry uint64
	for r := a.nd - 1; r >= 0; r-- {
		v := uint64(a.d[r]-'0')<<k + carry
		carry = v / 10
		w--
		buf[w] = byte(v-carry*10) + '0'
	}
	for carry > 0 {
		q := carry / 10
		w--
		buf[w] = byte(carry-q*10) + '0'
		carry = q
	}
	n := len(buf) - w
	a.dp += n - a.nd
	if n > len(a.d) {
		for _, c := range buf[w+len(a.d):] {
			if c != '0' {
				a.trunc = true
			}
		}
		n = len(a.d)
	}
	copy(a.d[:], buf[w:w+n])
	a.nd = n
	a.trim()
}

// shift multiplies a by 2^k (divides for k < 0).
func (a *fdecimal) shift(k int) {
	switch {
	case a.nd == 0:
	case k > 0:
		for k > fmaxShift {
			a.leftShift(fmaxShift)
			k -= fmaxShift
		}
		a.leftShift(uint(k))
	case k < 0:
		for k < -fmaxShift {
			a.rightShift(fmaxShift)
			k += fmaxShift
		}
		a.rightShift(uint(-k))
	}
}

func (a *fdecimal) shouldRoundUp(nd int) bool {
	if a.d[nd] == '5' && nd+1 == a.nd { // exactly halfway: round to even
		if a.trunc {
			return true
		}
		return nd > 0 && (a.d[nd-1]-'0')%2 == 1
	}
	return a.d[nd] >= '5'
}

func (a *fdecimal) round(nd int) {
	if nd < 0 || nd >= a.nd {
		return
	}
	if a.shouldRoundUp(nd) {
		a.roundUp(nd)
	} else {
		a.roundDown(nd)
	}
}

func (a *fdecimal) roundDown(nd int) {
	if nd < 0 || nd >= a.nd {
		return
	}
	a.nd = nd
	a.trim()
}

func (a *fdecimal) roundUp(nd int) {
	if nd < 0 || nd >= a.nd {
		return
	}
	for i := nd - 1; i >= 0; i-- {
		if a.d[i] < '9' {
			a.d[i]++
			a.nd = i + 1
			return
		}
	}
	// Number is all 9s: change to a single 1 with an adjusted decimal point.
	a.d[0] = '1'
	a.nd = 1
	a.dp++
}

type floatInfo struct {
	mantbits uint
	expbits  uint
	bias     int
}

var (
	float32info = floatInfo{23, 8, -127}
	float64info = floatInfo{52, 11, -1023}
)

// roundShortest rounds d (= mant * 2^(exp-mantbits)) to the shortest
// number of digits that still uniquely identifies the float.
func roundShortest(d *fdecimal, mant uint64, exp int, flt *floatInfo) {
	if mant == 0 {
		d.nd = 0
		return
	}

	// If mantissa scale is such that it is already the shortest, stop.
	minexp := flt.bias + 1 // minimum possible exponent
	if exp > minexp && 332*(d.dp-d.nd) >= 100*(exp-int(flt.mantbits)) {
		return
	}

	// d = mant << (exp - mantbits). Next highest floating point number is
	// mant+1 << exp-mantbits; upper is halfway between.
	var upper fdecimal
	upper.assign(mant*2 + 1)
	upper.shift(exp - int(flt.mantbits) - 1)

	// Next lowest floating point number is mant-1 << exp-mantbits, unless
	// mant-1 drops the significant bit and exp is not the minimum exp, in
	// which case the next lowest is mant*2-1 << exp-mantbits-1.
	var mantlo uint64
	var explo int
	if mant > 1<<flt.mantbits || exp == minexp {
		mantlo = mant - 1
		explo = exp
	} else {
		mantlo = mant*2 - 1
		explo = exp - 1
	}
	var lower fdecimal
	lower.assign(mantlo*2 + 1)
	lower.shift(explo - int(flt.mantbits) - 1)

	// The upper and lower bounds are possible outputs only if the original
	// mantissa is even, so that IEEE round-to-even would round to the
	// original mantissa and not the neighbors.
	inclusive := mant%2 == 0

	// As we walk the digits we want to know whether rounding up would
	// fall within the upper bound, tracked by upperdelta:
	//   0 if the digits of d and upper are the same so far,
	//   1 if we saw a difference of 1 between d and upper on the last
	//     digit, followed by 9s in d and 0s in upper,
	//   2 if we saw a difference larger than 1.
	var upperdelta uint8

	for ui := 0; ; ui++ {
		mi := ui - upper.dp + d.dp
		if mi >= d.nd {
			break
		}
		li := ui - upper.dp + lower.dp
		l := byte('0')
		if li >= 0 && li < lower.nd {
			l = lower.d[li]
		}
		m := byte('0')
		if mi >= 0 {
			m = d.d[mi]
		}
		u := byte('0')
		if ui < upper.nd {
			u = upper.d[ui]
		}

		// Okay to round down (truncate) if lower has a different digit or
		// if lower is inclusive and is exactly the result of rounding down
		// (i.e., and we have reached the final digit of lower).
		okdown := l != m || inclusive && li+1 == lower.nd

		switch {
		case upperdelta == 0 && m+1 < u:
			upperdelta = 2
		case upperdelta == 0 && m != u:
			upperdelta = 1
		case upperdelta == 1 && (m != '9' || u != '0'):
			upperdelta = 2
		}
		// Okay to round up if upper has a different digit and either
		// upper is inclusive or upper is bigger than the result of
		// rounding up.
		okup := upperdelta > 0 && (inclusive || upperdelta > 1 || ui+1 < upper.nd)

		switch {
		case okdown && okup:
			d.round(mi + 1)
			return
		case okdown:
			d.roundDown(mi + 1)
			return
		case okup:
			d.roundUp(mi + 1)
			return
		}
	}
}

// appendFloat appends the shortest 'g' formatting of v (as a float of
// bitSize 32 or 64) to dst, like strconv.AppendFloat(dst, v, 'g', -1,
// bitSize).
func appendFloat(dst []byte, v float64, bitSize int) []byte {
	var bits uint64
	var flt *floatInfo
	if bitSize == 32 {
		f := float32(v)
		bits = uint64(*(*uint32)(unsafe.Pointer(&f)))
		flt = &float32info
	} else {
		bits = *(*uint64)(unsafe.Pointer(&v))
		flt = &float64info
	}

	neg := bits>>(flt.expbits+flt.mantbits) != 0
	exp := int(bits>>flt.mantbits) & (1<<flt.expbits - 1)
	mant := bits & (uint64(1)<<flt.mantbits - 1)

	switch exp {
	case 1<<flt.expbits - 1:
		// Inf, NaN
		switch {
		case mant != 0:
			return append(dst, "NaN"...)
		case neg:
			return append(dst, "-Inf"...)
		}
		return append(dst, "+Inf"...)
	case 0:
		// denormalized
		exp++
	default:
		// add implicit top bit
		mant |= uint64(1) << flt.mantbits
	}
	exp += flt.bias

	var d fdecimal
	d.assign(mant)
	d.shift(exp - int(flt.mantbits))
	roundShortest(&d, mant, exp, flt)

	if neg {
		dst = append(dst, '-')
	}
	// %e is used if the exponent from the conversion is less than -4 or
	// greater than or equal to the precision; shortest uses precision 6
	// here, as strconv does for 'g' with precision -1.
	eprec := 6
	x := d.dp - 1
	if x < -4 || x >= eprec {
		return fmtE(dst, &d)
	}
	return fmtF(dst, &d)
}

// fmtE formats d as d.ddde±dd.
func fmtE(dst []byte, d *fdecimal) []byte {
	ch := byte('0')
	if d.nd != 0 {
		ch = d.d[0]
	}
	dst = append(dst, ch)
	if d.nd > 1 {
		dst = append(dst, '.')
		dst = append(dst, d.d[1:d.nd]...)
	}
	dst = append(dst, 'e')
	exp := d.dp - 1
	if d.nd == 0 { // special case: 0 has exponent 0
		exp = 0
	}
	ch = '+'
	if exp < 0 {
		ch = '-'
		exp = -exp
	}
	dst = append(dst, ch)
	switch {
	case exp < 10:
		dst = append(dst, '0', byte(exp)+'0')
	case exp < 100:
		dst = append(dst, byte(exp/10)+'0', byte(exp%10)+'0')
	default:
		dst = append(dst, byte(exp/100)+'0', byte(exp/10)%10+'0', byte(exp%10)+'0')
	}
	return dst
}

// fmtF formats d as ddd.ddd with exactly the digits of d.
func fmtF(dst []byte, d *fdecimal) []byte {
	// integer, padded with zeros as needed.
	if d.dp > 0 {
		m := min(d.nd, d.dp)
		dst = append(dst, d.d[:m]...)
		for ; m < d.dp; m++ {
			dst = append(dst, '0')
		}
	} else {
		dst = append(dst, '0')
	}
	// fraction
	prec := d.nd - d.dp
	if prec > 0 {
		dst = append(dst, '.')
		for i := 1; i <= prec; i++ {
			ch := byte('0')
			if j := d.dp + i - 1; 0 <= j && j < d.nd {
				ch = d.d[j]
			}
			dst = append(dst, ch)
		}
	}
	return dst
}

// appendComplex appends (re±imi) like strconv.AppendComplex(dst, c, 'g',
// -1, bitSize) with bitSize 64 (complex64) or 128.
func appendComplex(dst []byte, c complex128, bitSize int) []byte {
	fbits := 64
	if bitSize == 64 {
		fbits = 32
	}
	dst = append(dst, '(')
	dst = appendFloat(dst, real(c), fbits)
	i := len(dst)
	dst = appendFloat(dst, imag(c), fbits)
	// Add a + sign if needed: imaginary parts other than -x and +Inf get
	// one.
	if dst[i] != '+' && dst[i] != '-' {
		dst = append(dst, 0)
		copy(dst[i+1:], dst[i:])
		dst[i] = '+'
	}
	return append(dst, "i)"...)
}
