// UTF-8 decoding and encoding, as in unicode/utf8. Pure file.

package runtime

const (
	runeError = '�'
	runeSelf  = 0x80
	maxRune   = '\U0010FFFF'

	surrogateMin = 0xD800
	surrogateMax = 0xDFFF

	t1 = 0x00 // 0000 0000
	tx = 0x80 // 1000 0000
	t2 = 0xC0 // 1100 0000
	t3 = 0xE0 // 1110 0000
	t4 = 0xF0 // 1111 0000
	t5 = 0xF8 // 1111 1000

	maskx = 0x3F // 0011 1111
	mask2 = 0x1F // 0001 1111
	mask3 = 0x0F // 0000 1111
	mask4 = 0x07 // 0000 0111

	rune1Max = 1<<7 - 1
	rune2Max = 1<<11 - 1
	rune3Max = 1<<16 - 1

	locb = 0x80 // 1000 0000
	hicb = 0xBF // 1011 1111
)

// decoderune returns the rune at s[k] and the index just after it. For an
// invalid encoding it returns (runeError, k+1), like range over a string.
// It requires 0 <= k < len(s). The compiler may handle the ASCII case
// inline and call this only when s[k] >= 0x80; both uses are correct.
func decoderune(s string, k int) (r rune, pos int) {
	pos = k
	if k >= len(s) {
		return runeError, k + 1
	}
	s = s[k:]
	c0 := s[0]
	if c0 < runeSelf {
		return rune(c0), k + 1
	}
	n := len(s)
	switch {
	case t2 <= c0 && c0 < t3:
		// 0080-07FF two byte sequence
		if n > 1 && (locb <= s[1] && s[1] <= hicb) {
			r = rune(c0&mask2)<<6 | rune(s[1]&maskx)
			pos += 2
			if rune1Max < r {
				return
			}
		}
	case t3 <= c0 && c0 < t4:
		// 0800-FFFF three byte sequence
		if n > 2 && (locb <= s[1] && s[1] <= hicb) && (locb <= s[2] && s[2] <= hicb) {
			r = rune(c0&mask3)<<12 | rune(s[1]&maskx)<<6 | rune(s[2]&maskx)
			pos += 3
			if rune2Max < r && !(surrogateMin <= r && r <= surrogateMax) {
				return
			}
		}
	case t4 <= c0 && c0 < t5:
		// 10000-1FFFFF four byte sequence
		if n > 3 && (locb <= s[1] && s[1] <= hicb) && (locb <= s[2] && s[2] <= hicb) && (locb <= s[3] && s[3] <= hicb) {
			r = rune(c0&mask4)<<18 | rune(s[1]&maskx)<<12 | rune(s[2]&maskx)<<6 | rune(s[3]&maskx)
			pos += 4
			if rune3Max < r && r <= maxRune {
				return
			}
		}
	}
	return runeError, k + 1
}

// encoderune writes the UTF-8 encoding of r into p (which must be large
// enough) and returns the number of bytes written. Invalid runes encode as
// runeError.
func encoderune(p []byte, r rune) int {
	switch i := uint32(r); {
	case i <= rune1Max:
		p[0] = byte(r)
		return 1
	case i <= rune2Max:
		_ = p[1]
		p[0] = t2 | byte(r>>6)
		p[1] = tx | byte(r)&maskx
		return 2
	case i > maxRune, surrogateMin <= i && i <= surrogateMax:
		r = runeError
		fallthrough
	case i <= rune3Max:
		_ = p[2]
		p[0] = t3 | byte(r>>12)
		p[1] = tx | byte(r>>6)&maskx
		p[2] = tx | byte(r)&maskx
		return 3
	default:
		_ = p[3]
		p[0] = t4 | byte(r>>18)
		p[1] = tx | byte(r>>12)&maskx
		p[2] = tx | byte(r>>6)&maskx
		p[3] = tx | byte(r)&maskx
		return 4
	}
}

// runelen returns the number of bytes encoderune writes for r.
func runelen(r rune) int {
	switch {
	case r < 0:
		return 3
	case r <= rune1Max:
		return 1
	case r <= rune2Max:
		return 2
	case surrogateMin <= r && r <= surrogateMax:
		return 3
	case r <= rune3Max:
		return 3
	case r <= maxRune:
		return 4
	}
	return 3
}

// countrunes returns the number of runes in s, as len([]rune(s)).
func countrunes(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] < runeSelf {
			i++
		} else {
			_, i = decoderune(s, i)
		}
		n++
	}
	return n
}
