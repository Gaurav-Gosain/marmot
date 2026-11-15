package diff

func Add8(a, b int8) int8         { return a + b }
func Sub16(a, b uint16) uint16    { return a - b }
func Mul32(a, b int32) int32      { return a * b }
func Div(a, b int) int            { return a / b }
func Rem(a, b int64) int64        { return a % b }
func UDiv(a, b uint32) uint32     { return a / b }
func Shl(x uint64, n uint) uint64 { return x << n }
func Shr(x int16, n uint8) int16  { return x >> n }
func ShrU(x uint8, n int) uint8   { return x >> n }
func Conv(x int64) int32          { return int32(x) + 1 }
func ConvU(x int8) uint64         { return uint64(x) }
func Mix(a int, b uint8) int {
	c := int(b) * 3
	if a > c {
		return a - c
	}
	return c &^ a
}
func Branch(x int32) int32 {
	r := x
	if x < 0 {
		r = -x
	} else if x > 100 {
		r = x / 7
	}
	return r ^ 5
}
func Min(a, b int) int        { return min(a, b, 7) }
func Max(a, b uint16) uint16  { return max(a, b) }
func At(b []byte, i int) byte { return b[i] }
func Sub(b []byte, lo, hi int) int {
	s := b[lo:hi]
	return len(s)*10 + cap(s)
}
func Head(b []byte) int {
	if len(b) < 2 {
		return -1
	}
	return int(b[0])<<8 | int(b[1])
}
func StrAt(s string, i uint) byte { return s[i] }
func StrTail(s string, n int) int { return len(s[n:]) }
func Full(b []byte, i, j, k int) int {
	t := b[i:j:k]
	return len(t) + 100*cap(t)
}
func Last(b []byte) byte {
	if len(b) == 0 {
		return 0
	}
	return b[len(b)-1]
}
func Bools(a, b bool, x int) int {
	if a == b {
		return x
	}
	if a && !b {
		return -x
	}
	return 0
}
