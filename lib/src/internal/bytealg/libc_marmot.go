// marmot overlay of internal/bytealg: the assembly versions are replaced by
// calls to the C library (memchr, memcmp), which is vectorized and which
// LLVM knows by name. The other operations come from the stock *_generic.go
// files (copied as *_marmot.go without their build constraints).

package bytealg

import "unsafe"

//go:linkname libc_memchr memchr
func libc_memchr(s unsafe.Pointer, c int32, n uintptr) unsafe.Pointer

//go:linkname libc_memcmp memcmp
func libc_memcmp(a, b unsafe.Pointer, n uintptr) int32

// IndexByte returns the index of the first instance of c in b, or -1.
func IndexByte(b []byte, c byte) int {
	if len(b) == 0 {
		return -1
	}
	p := unsafe.Pointer(unsafe.SliceData(b))
	r := libc_memchr(p, int32(c), uintptr(len(b)))
	if r == nil {
		return -1
	}
	return int(uintptr(r) - uintptr(p))
}

// IndexByteString returns the index of the first instance of c in s, or -1.
func IndexByteString(s string, c byte) int {
	if len(s) == 0 {
		return -1
	}
	p := unsafe.Pointer(unsafe.StringData(s))
	r := libc_memchr(p, int32(c), uintptr(len(s)))
	if r == nil {
		return -1
	}
	return int(uintptr(r) - uintptr(p))
}

// Equal reports whether a and b are the same length and contain the same
// bytes. A nil argument is equivalent to an empty slice.
func Equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return libc_memcmp(unsafe.Pointer(unsafe.SliceData(a)), unsafe.Pointer(unsafe.SliceData(b)), uintptr(len(a))) == 0
}

// Compare returns -1, 0 or +1 comparing a and b bytewise.
func Compare(a, b []byte) int {
	l := len(a)
	if len(b) < l {
		l = len(b)
	}
	if l > 0 {
		pa := unsafe.Pointer(unsafe.SliceData(a))
		pb := unsafe.Pointer(unsafe.SliceData(b))
		if pa != pb {
			c := libc_memcmp(pa, pb, uintptr(l))
			if c < 0 {
				return -1
			}
			if c > 0 {
				return +1
			}
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return +1
	}
	return 0
}

// CompareString is Compare for strings.
func CompareString(a, b string) int {
	return Compare(unsafe.Slice(unsafe.StringData(a), len(a)), unsafe.Slice(unsafe.StringData(b), len(b)))
}
