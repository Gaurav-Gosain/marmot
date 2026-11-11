// Runtime error values and the panic entry points the compiler calls for
// failed checks. Messages match the gc runtime exactly. Pure file.

package runtime

// The Error interface identifies a run time error.
type Error interface {
	error

	// RuntimeError is a no-op function but serves to distinguish types
	// that are run time errors from ordinary errors.
	RuntimeError()
}

// errorString is a runtime error whose message is prefixed with
// "runtime error: ".
type errorString string

func (e errorString) RuntimeError() {}

func (e errorString) Error() string {
	return "runtime error: " + string(e)
}

// plainError is a runtime error without the "runtime error: " prefix.
type plainError string

func (e plainError) RuntimeError() {}

func (e plainError) Error() string {
	return string(e)
}

// A boundsError is an out of bounds index or slice expression.
type boundsError struct {
	x int64
	y int
	// signed reports whether x is signed. Values of unsigned types that do
	// not fit in int64 are reported as unsigned.
	signed bool
	code   uint8
}

// Codes for boundsError and panicBounds. The compiler checks slice
// expressions in this order and reports the first failure:
//
//	s[x]            x < len                 boundsIndex
//	s[lo:hi]        hi <= cap (slices)      boundsSliceAcap (x=hi, y=cap)
//	                hi <= len (strings,     boundsSliceAlen (x=hi, y=len)
//	                arrays)
//	                lo <= hi                boundsSliceB    (x=lo, y=hi)
//	s[lo:hi:max]    max <= cap              boundsSlice3Acap (x=max, y=cap)
//	                hi <= max               boundsSlice3B    (x=hi, y=max)
//	                lo <= hi                boundsSlice3C    (x=lo, y=hi)
//	[N]T(s)         N <= len                boundsConvert    (x=len, y=N)
const (
	boundsIndex      = 0
	boundsSliceAlen  = 1
	boundsSliceAcap  = 2
	boundsSliceB     = 3
	boundsSlice3Alen = 4
	boundsSlice3Acap = 5
	boundsSlice3B    = 6
	boundsSlice3C    = 7
	boundsConvert    = 8
)

func (e boundsError) RuntimeError() {}

// Error formats the message without fmt. %x is e.x and %y is e.y.
func (e boundsError) Error() string {
	var b [96]byte
	n := 0
	if e.signed && e.x < 0 {
		n = appendBoundsNeg(b[:], e)
	} else {
		n = appendBounds(b[:], e)
	}
	return "runtime error: " + string(b[:n])
}

func appendBounds(b []byte, e boundsError) int {
	n := 0
	switch e.code {
	case boundsIndex:
		n = appendStr(b, n, "index out of range [")
		n = appendX(b, n, e)
		n = appendStr(b, n, "] with length ")
		n = appendInt(b, n, int64(e.y))
	case boundsSliceAlen:
		n = appendStr(b, n, "slice bounds out of range [:")
		n = appendX(b, n, e)
		n = appendStr(b, n, "] with length ")
		n = appendInt(b, n, int64(e.y))
	case boundsSliceAcap:
		n = appendStr(b, n, "slice bounds out of range [:")
		n = appendX(b, n, e)
		n = appendStr(b, n, "] with capacity ")
		n = appendInt(b, n, int64(e.y))
	case boundsSliceB:
		n = appendStr(b, n, "slice bounds out of range [")
		n = appendX(b, n, e)
		n = appendStr(b, n, ":")
		n = appendInt(b, n, int64(e.y))
		n = appendStr(b, n, "]")
	case boundsSlice3Alen:
		n = appendStr(b, n, "slice bounds out of range [::")
		n = appendX(b, n, e)
		n = appendStr(b, n, "] with length ")
		n = appendInt(b, n, int64(e.y))
	case boundsSlice3Acap:
		n = appendStr(b, n, "slice bounds out of range [::")
		n = appendX(b, n, e)
		n = appendStr(b, n, "] with capacity ")
		n = appendInt(b, n, int64(e.y))
	case boundsSlice3B:
		n = appendStr(b, n, "slice bounds out of range [:")
		n = appendX(b, n, e)
		n = appendStr(b, n, ":")
		n = appendInt(b, n, int64(e.y))
		n = appendStr(b, n, "]")
	case boundsSlice3C:
		n = appendStr(b, n, "slice bounds out of range [")
		n = appendX(b, n, e)
		n = appendStr(b, n, ":")
		n = appendInt(b, n, int64(e.y))
		n = appendStr(b, n, ":]")
	case boundsConvert:
		n = appendStr(b, n, "cannot convert slice with length ")
		n = appendX(b, n, e)
		n = appendStr(b, n, " to array or pointer to array with length ")
		n = appendInt(b, n, int64(e.y))
	}
	return n
}

// appendBoundsNeg formats the messages used when x is negative.
func appendBoundsNeg(b []byte, e boundsError) int {
	n := 0
	switch e.code {
	case boundsIndex:
		n = appendStr(b, n, "index out of range [")
		n = appendX(b, n, e)
		n = appendStr(b, n, "]")
	case boundsSliceAlen, boundsSliceAcap:
		n = appendStr(b, n, "slice bounds out of range [:")
		n = appendX(b, n, e)
		n = appendStr(b, n, "]")
	case boundsSliceB:
		n = appendStr(b, n, "slice bounds out of range [")
		n = appendX(b, n, e)
		n = appendStr(b, n, ":]")
	case boundsSlice3Alen, boundsSlice3Acap:
		n = appendStr(b, n, "slice bounds out of range [::")
		n = appendX(b, n, e)
		n = appendStr(b, n, "]")
	case boundsSlice3B:
		n = appendStr(b, n, "slice bounds out of range [:")
		n = appendX(b, n, e)
		n = appendStr(b, n, ":]")
	case boundsSlice3C:
		n = appendStr(b, n, "slice bounds out of range [")
		n = appendX(b, n, e)
		n = appendStr(b, n, "::]")
	case boundsConvert:
		// Not reachable: lengths are never negative.
		n = appendStr(b, n, "cannot convert slice with length ")
		n = appendX(b, n, e)
		n = appendStr(b, n, " to array or pointer to array with length ")
		n = appendInt(b, n, int64(e.y))
	}
	return n
}

func appendStr(b []byte, n int, s string) int {
	return n + copy(b[n:], s)
}

func appendX(b []byte, n int, e boundsError) int {
	if e.signed {
		return appendInt(b, n, e.x)
	}
	return appendUint(b, n, uint64(e.x))
}

func appendInt(b []byte, n int, v int64) int {
	if v < 0 {
		b[n] = '-'
		n++
		return appendUint(b, n, uint64(-v))
	}
	return appendUint(b, n, uint64(v))
}

func appendUint(b []byte, n int, v uint64) int {
	var buf [20]byte
	i := len(buf)
	for {
		i--
		buf[i] = byte(v%10) + '0'
		v /= 10
		if v == 0 {
			break
		}
	}
	return n + copy(b[n:], buf[i:])
}

// itoa formats v as a decimal string.
func itoa(v int64) string {
	var b [24]byte
	n := appendInt(b[:], 0, v)
	return string(b[:n])
}

// A TypeAssertionError explains a failed type assertion.
type TypeAssertionError struct {
	_interface    *_type
	concrete      *_type
	asserted      *_type
	missingMethod string // one method needed by Interface, missing from Concrete
}

func (*TypeAssertionError) RuntimeError() {}

func (e *TypeAssertionError) Error() string {
	inter := "interface"
	if e._interface != nil {
		inter = e._interface.str
	}
	as := e.asserted.str
	if e.concrete == nil {
		return "interface conversion: " + inter + " is nil, not " + as
	}
	cs := e.concrete.str
	if e.missingMethod == "" {
		msg := "interface conversion: " + inter + " is " + cs + ", not " + as
		if cs == as {
			// provide slightly clearer error message
			if typePkgPath(e.concrete) != typePkgPath(e.asserted) {
				msg += " (types from different packages)"
			} else {
				msg += " (types from different scopes)"
			}
		}
		return msg
	}
	return "interface conversion: " + cs + " is not " + as +
		": missing method " + e.missingMethod
}

func typePkgPath(t *_type) string {
	if t.uncommon != nil {
		return t.uncommon.pkgpath
	}
	return ""
}

// A PanicNilError happens when code calls panic(nil).
type PanicNilError struct {
	_ [0]*PanicNilError
}

func (*PanicNilError) Error() string { return "panic called with nil argument" }
func (*PanicNilError) RuntimeError() {}

// Entry points for compiler generated checks. None of them return.

// panicBounds reports a failed bounds check. code is one of the bounds
// constants above; signed reports whether x came from a signed type.
func panicBounds(code int, x int64, signed bool, y int) {
	panic(boundsError{x: x, signed: signed, y: y, code: uint8(code)})
}

// panicindex reports s[x] with x out of range for length y (signed index).
func panicindex(x int, y int) {
	panic(boundsError{x: int64(x), signed: true, y: y, code: boundsIndex})
}

// panicindexU is panicindex for an unsigned index.
func panicindexU(x uint, y int) {
	panic(boundsError{x: int64(x), signed: false, y: y, code: boundsIndex})
}

// panicslice reports a failed check of s[lo:hi] (max is the capacity, or
// the length for strings and arrays). It cannot tell capacity from length;
// the compiler should prefer panicBounds, which reports the exact message.
func panicslice(lo, hi, max int) {
	if uint(hi) > uint(max) {
		panic(boundsError{x: int64(hi), signed: true, y: max, code: boundsSliceAcap})
	}
	panic(boundsError{x: int64(lo), signed: true, y: hi, code: boundsSliceB})
}

// panicslice3 reports a failed check of s[lo:hi:max] with capacity c.
func panicslice3(lo, hi, max, c int) {
	if uint(max) > uint(c) {
		panic(boundsError{x: int64(max), signed: true, y: c, code: boundsSlice3Acap})
	}
	if uint(hi) > uint(max) {
		panic(boundsError{x: int64(hi), signed: true, y: max, code: boundsSlice3B})
	}
	panic(boundsError{x: int64(lo), signed: true, y: hi, code: boundsSlice3C})
}

// panicSliceConvert reports a failed conversion of a slice of length have
// to an array (or pointer to array) of length want.
func panicSliceConvert(want, have int) {
	panic(boundsError{x: int64(have), signed: true, y: want, code: boundsConvert})
}

func panicdivide() {
	panic(errorString("integer divide by zero"))
}

func panicoverflow() {
	panic(errorString("integer overflow"))
}

func panicshift() {
	panic(errorString("negative shift amount"))
}

// panicmem reports a nil pointer dereference.
func panicmem() {
	panic(errorString("invalid memory address or nil pointer dereference"))
}

// panicnil is the doc name for panicmem.
func panicnil() {
	panic(errorString("invalid memory address or nil pointer dereference"))
}

func panicmakeslicelen() {
	panic(errorString("makeslice: len out of range"))
}

func panicmakeslicecap() {
	panic(errorString("makeslice: cap out of range"))
}

func panicunsafeslicelen() {
	panic(errorString("unsafe.Slice: len out of range"))
}

func panicunsafeslicenilptr() {
	panic(errorString("unsafe.Slice: ptr is nil and len is not zero"))
}

func panicunsafestringlen() {
	panic(errorString("unsafe.String: len out of range"))
}

func panicunsafestringnilptr() {
	panic(errorString("unsafe.String: ptr is nil and len is not zero"))
}

// panicnilmap reports an assignment to an entry in a nil map.
func panicnilmap() {
	panic(plainError("assignment to entry in nil map"))
}
