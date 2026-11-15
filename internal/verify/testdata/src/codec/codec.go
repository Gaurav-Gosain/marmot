package codec

// ReadU16 reads a big-endian uint16 at off.
func ReadU16(b []byte, off int) uint16 {
	return uint16(b[off])<<8 | uint16(b[off+1]) // want "`Header` can panic: index out of range when len\\(b\\) == 2"
}

func Header(b []byte) (kind byte, n uint16) {
	if len(b) < 2 {
		return 0, 0
	}
	return b[0], ReadU16(b, 1)
}

func Sum(s []int) int {
	t := 0
	for i := 0; i < len(s); i++ {
		t += s[i]
	}
	return t
}

func SumRange(s []int) int {
	t := 0
	for i := range s {
		t += s[i]
	}
	for _, v := range s {
		t += v
	}
	return t
}

func Pairs(s []int) int {
	t := 0
	for i := 0; i < len(s)-1; i++ {
		t += s[i] * s[i+1]
	}
	return t
}

func Pairs2(s []int) int {
	t := 0
	for i := 1; i < len(s); i++ {
		t += s[i-1] - s[i]
	}
	return t
}

func OffByOne(s []int) int {
	t := 0
	for i := 0; i <= len(s); i++ {
		t += s[i] // want "`OffByOne` can panic: index out of range when i == len\\(s\\)"
	}
	return t
}

func Guarded(s []byte, i int) byte {
	if i < 0 || i >= len(s) {
		return 0
	}
	return s[i]
}

func GuardedWrong(s []byte, i int) byte {
	if i < 0 || i > len(s) {
		return 0
	}
	return s[i] // want "`GuardedWrong` can panic: index out of range when i == len\\(s\\)"
}

func First4(b []byte) []byte {
	if len(b) < 3 {
		return nil
	}
	return b[:4]
}

func Mod(s []int, h int) int {
	if len(s) == 0 {
		return 0
	}
	return s[uint(h)%uint(len(s))]
}

func Div(a, b int) int {
	if b == 0 {
		return 0
	}
	return a / b
}

func At(s []int, i int) int { return s[i] }

type T struct{ x int }

func (t *T) X() int { return t.x }

func Ptr(p *T) int {
	if p == nil {
		return 0
	}
	return p.x
}

func Search(s []int, x int) int {
	lo, hi := 0, len(s)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		switch {
		case s[m] < x:
			lo = m + 1
		case s[m] > x:
			hi = m
		default:
			return m
		}
	}
	return -1
}

func Rev(s []int) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func Str(s string) byte {
	if s == "" {
		return 0
	}
	return s[0]
}

func Arr(i int) int {
	var a [8]int
	if i > 8 {
		return 0
	}
	return a[i] // want "`Arr` can panic: index out of range when i == 8"
}

func Back(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == ' ' {
		b = b[:len(b)-1]
	}
	return b
}

func Skip(b []byte, n int) []byte {
	if n > len(b) {
		return nil
	}
	return b[n:]
}
