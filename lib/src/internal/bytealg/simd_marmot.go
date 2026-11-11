// marmot overlay of internal/bytealg: the assembly versions are replaced by
// the C core's SIMD primitives (NEON on arm64, SSE2 on x86-64), which LLVM
// can inline from rtcore.bc. Results are identical to the Go versions.
// MaxLen and MaxBruteForce are set so that strings.Index and bytes.Index
// hand every search to Index/IndexString.

package bytealg

import "unsafe"

//go:linkname rt_index_byte rt_index_byte
func rt_index_byte(p unsafe.Pointer, n int, c uint8) int

//go:linkname rt_last_index_byte rt_last_index_byte
func rt_last_index_byte(p unsafe.Pointer, n int, c uint8) int

//go:linkname rt_count_byte rt_count_byte
func rt_count_byte(p unsafe.Pointer, n int, c uint8) int

//go:linkname rt_mem_equal rt_mem_equal
func rt_mem_equal(a, b unsafe.Pointer, n int) int32

//go:linkname rt_compare rt_compare
func rt_compare(a unsafe.Pointer, an int, b unsafe.Pointer, bn int) int32

//go:linkname rt_index rt_index
func rt_index(p unsafe.Pointer, n int, sep unsafe.Pointer, m int) int

func sliceData(b []byte) unsafe.Pointer  { return unsafe.Pointer(unsafe.SliceData(b)) }
func stringData(s string) unsafe.Pointer { return unsafe.Pointer(unsafe.StringData(s)) }

// MaxBruteForce: strings.Index and bytes.Index call Index directly for any
// haystack of at most this length, which is every haystack.
const MaxBruteForce = 1 << 62

func init() {
	// Index handles needles of any length.
	MaxLen = 1 << 62
}

// Index returns the index of the first instance of b in a, or -1 if b is
// not present in a.
func Index(a, b []byte) int {
	return rt_index(sliceData(a), len(a), sliceData(b), len(b))
}

// IndexString returns the index of the first instance of b in a, or -1 if
// b is not present in a.
func IndexString(a, b string) int {
	return rt_index(stringData(a), len(a), stringData(b), len(b))
}

// Cutover is never consulted (MaxBruteForce covers every length).
func Cutover(n int) int {
	return (n + 16) / 8
}

// IndexByte returns the index of the first instance of c in b, or -1.
func IndexByte(b []byte, c byte) int {
	return rt_index_byte(sliceData(b), len(b), c)
}

// IndexByteString returns the index of the first instance of c in s, or -1.
func IndexByteString(s string, c byte) int {
	return rt_index_byte(stringData(s), len(s), c)
}

// LastIndexByte returns the index of the last instance of c in s, or -1.
func LastIndexByte(s []byte, c byte) int {
	return rt_last_index_byte(sliceData(s), len(s), c)
}

// LastIndexByteString returns the index of the last instance of c in s, or
// -1.
func LastIndexByteString(s string, c byte) int {
	return rt_last_index_byte(stringData(s), len(s), c)
}

// Count returns the number of instances of c in b.
func Count(b []byte, c byte) int {
	return rt_count_byte(sliceData(b), len(b), c)
}

// CountString returns the number of instances of c in s.
func CountString(s string, c byte) int {
	return rt_count_byte(stringData(s), len(s), c)
}

// Equal reports whether a and b are the same length and contain the same
// bytes. A nil argument is equivalent to an empty slice.
func Equal(a, b []byte) bool {
	return len(a) == len(b) && rt_mem_equal(sliceData(a), sliceData(b), len(a)) != 0
}

// Compare returns -1, 0 or +1 comparing a and b bytewise.
func Compare(a, b []byte) int {
	return int(rt_compare(sliceData(a), len(a), sliceData(b), len(b)))
}

// CompareString is Compare for strings.
func CompareString(a, b string) int {
	return int(rt_compare(stringData(a), len(a), stringData(b), len(b)))
}

//go:linkname rt_last_index rt_last_index
func rt_last_index(p unsafe.Pointer, n int, sep unsafe.Pointer, m int) int

//go:linkname rt_count_substr rt_count_substr
func rt_count_substr(p unsafe.Pointer, n int, sep unsafe.Pointer, m int) int

// MarmotLastIndexString returns the index of the last instance of sep in
// s, or -1 (len(s) if sep is empty), as strings.LastIndex.
func MarmotLastIndexString(s, sep string) int {
	return rt_last_index(stringData(s), len(s), stringData(sep), len(sep))
}

// MarmotLastIndex is MarmotLastIndexString for byte slices.
func MarmotLastIndex(s, sep []byte) int {
	return rt_last_index(sliceData(s), len(s), sliceData(sep), len(sep))
}

// MarmotCountString counts non-overlapping instances of sep (len(sep) >= 1)
// in s, as strings.Count.
func MarmotCountString(s, sep string) int {
	return rt_count_substr(stringData(s), len(s), stringData(sep), len(sep))
}

// MarmotCount is MarmotCountString for byte slices.
func MarmotCount(s, sep []byte) int {
	return rt_count_substr(sliceData(s), len(s), sliceData(sep), len(sep))
}

//go:linkname rt_replace_into rt_replace_into
func rt_replace_into(dst unsafe.Pointer, src unsafe.Pointer, n int, old unsafe.Pointer, m int, new unsafe.Pointer, k int) int

// MarmotReplaceInto writes s with every non-overlapping instance of old
// (len(old) >= 1) replaced by new into dst, which has room for
// len(s) + count*(len(new)-len(old)) bytes, and returns the bytes written.
func MarmotReplaceInto(dst []byte, s, old, new string) int {
	return rt_replace_into(sliceData(dst), stringData(s), len(s), stringData(old), len(old), stringData(new), len(new))
}
