// String operations called by compiled code. Pure file.

package runtime

import "unsafe"

const maxAlloc = 1 << 47

// rawstring allocates storage for a new string of size bytes and returns the
// string and a byte slice over the same memory for filling it.
func rawstring(size int) (s string, b []byte) {
	p := mallocNoZero(uintptr(size)) // the caller fills all of b
	return unsafe.String((*byte)(p), size), unsafe.Slice((*byte)(p), size)
}

// concatstrings returns the concatenation of the strings in a.
func concatstrings(a []string) string {
	l := 0
	count := 0
	idx := 0
	for i, x := range a {
		n := len(x)
		if n == 0 {
			continue
		}
		if l+n < l {
			throw("string concatenation too long")
		}
		l += n
		count++
		idx = i
	}
	if count == 0 {
		return ""
	}
	// A single non-empty string needs no copy.
	if count == 1 {
		return a[idx]
	}
	s, b := rawstring(l)
	for _, x := range a {
		n := copy(b, x)
		b = b[n:]
	}
	return s
}

func concatstring2(a, b string) string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	l := len(a) + len(b)
	if l < len(a) {
		throw("string concatenation too long")
	}
	s, buf := rawstring(l)
	copy(buf, a)
	copy(buf[len(a):], b)
	return s
}

func concatstring3(a, b, c string) string {
	l := len(a) + len(b) + len(c)
	if l == 0 {
		return ""
	}
	s, buf := rawstring(l)
	n := copy(buf, a)
	n += copy(buf[n:], b)
	copy(buf[n:], c)
	return s
}

// cmpstring returns -1, 0 or +1 comparing a and b bytewise.
func cmpstring(a, b string) int {
	l := len(a)
	if len(b) < l {
		l = len(b)
	}
	if l > 0 {
		pa := unsafe.Pointer(unsafe.StringData(a))
		pb := unsafe.Pointer(unsafe.StringData(b))
		if pa != pb {
			c := memcmp(pa, pb, uintptr(l))
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

// strequal reports whether a == b. The compiler may compare the lengths
// itself and call memequal on the data instead.
func strequal(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return memequal(unsafe.Pointer(unsafe.StringData(a)), unsafe.Pointer(unsafe.StringData(b)), uintptr(len(a)))
}

// rawbyteslice allocates a byte slice of length size with the capacity the
// gc runtime would give it.
func rawbyteslice(size int) []byte {
	c := roundupsize(uintptr(size), true)
	p := mallocgc(c, nil)
	return unsafe.Slice((*byte)(p), c)[:size]
}

// stringtoslicebyte implements []byte(s).
func stringtoslicebyte(s string) []byte {
	b := rawbyteslice(len(s))
	copy(b, s)
	return b
}

// slicebytetostring implements string(b).
func slicebytetostring(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	s, buf := rawstring(len(b))
	copy(buf, b)
	return s
}

// slicebytetostringtmp returns a string that aliases b. The compiler may
// use it where the string does not outlive b and b is not modified, for
// example m[string(b)] lookups and string(b) == "x" comparisons.
func slicebytetostringtmp(p *byte, n int) string {
	return unsafe.String(p, n)
}

// rawruneslice allocates a rune slice of length size with the capacity the
// gc runtime would give it.
func rawruneslice(size int) []rune {
	if uintptr(size) > maxAlloc/4 {
		throw("out of memory")
	}
	mem := roundupsize(uintptr(size)*4, true)
	p := mallocgc(mem, nil)
	return unsafe.Slice((*rune)(p), mem/4)[:size]
}

// stringtoslicerune implements []rune(s).
func stringtoslicerune(s string) []rune {
	n := countrunes(s)
	a := rawruneslice(n)
	n = 0
	for i := 0; i < len(s); {
		r := rune(s[i])
		if r < runeSelf {
			i++
		} else {
			r, i = decoderune(s, i)
		}
		a[n] = r
		n++
	}
	return a
}

// slicerunetostring implements string(r).
func slicerunetostring(a []rune) string {
	size1 := 0
	for _, r := range a {
		size1 += runelen(r)
	}
	s, b := rawstring(size1)
	size2 := 0
	for _, r := range a {
		if size2 >= size1 {
			break
		}
		size2 += encoderune(b[size2:], r)
	}
	return s[:size2]
}

// intstring implements string(rune(v)) for any integer v. Values that are
// not valid code points give "�".
func intstring(v int64) string {
	r := rune(v)
	if int64(r) != v {
		r = runeError
	}
	s, b := rawstring(runelen(r))
	encoderune(b, r)
	return s
}
