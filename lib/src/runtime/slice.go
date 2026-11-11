// Slice allocation and growth. Pure file.

package runtime

import "unsafe"

// makeslice implements make([]T, len, cap) and returns the backing array.
// It panics with Go's messages when len or cap is negative or too large.
func makeslice(et *_type, len, cap int) unsafe.Pointer {
	mem, overflow := mulUintptr(et.size, uintptr(cap))
	if overflow || mem > maxAlloc || len < 0 || len > cap {
		// Report len out of range before cap out of range, as gc does.
		mem, overflow := mulUintptr(et.size, uintptr(len))
		if overflow || mem > maxAlloc || len < 0 {
			panicmakeslicelen()
		}
		panicmakeslicecap()
	}
	return mallocgc(mem, et)
}

// makeslice64 is makeslice for len and cap of type int64 or uint64 that may
// not fit in int.
func makeslice64(et *_type, len64, cap64 int64) unsafe.Pointer {
	len := int(len64)
	if int64(len) != len64 {
		panicmakeslicelen()
	}
	cap := int(cap64)
	if int64(cap) != cap64 {
		panicmakeslicecap()
	}
	return makeslice(et, len, cap)
}

// growslice implements append when the capacity is too small. oldPtr,
// oldLen and oldCap describe the slice before the append; newLen is the
// length after it. It returns a slice with length newLen whose first oldLen
// elements are copied from the old slice; the caller writes the appended
// elements at [oldLen:newLen].
func growslice(et *_type, oldPtr unsafe.Pointer, oldLen, oldCap, newLen int) slice {
	if newLen < 0 {
		panic(errorString("growslice: len out of range"))
	}
	if et.size == 0 {
		// append should not create a slice with nil pointer but non-zero
		// length. Use zerobase.
		return slice{mallocgc(0, nil), newLen, newLen}
	}

	newcap := nextslicecap(newLen, oldCap)

	var overflow bool
	var lenmem, newlenmem, capmem uintptr
	noscan := et.ptrdata == 0
	switch {
	case et.size == 1:
		lenmem = uintptr(oldLen)
		newlenmem = uintptr(newLen)
		capmem = roundupsize(uintptr(newcap), noscan)
		overflow = uintptr(newcap) > maxAlloc
		newcap = int(capmem)
	case et.size == ptrSize:
		lenmem = uintptr(oldLen) * ptrSize
		newlenmem = uintptr(newLen) * ptrSize
		capmem = roundupsize(uintptr(newcap)*ptrSize, noscan)
		overflow = uintptr(newcap) > maxAlloc/ptrSize
		newcap = int(capmem / ptrSize)
	case isPowerOfTwo(et.size):
		shift := uintptr(trailingZeros64(uint64(et.size)))
		lenmem = uintptr(oldLen) << shift
		newlenmem = uintptr(newLen) << shift
		capmem = roundupsize(uintptr(newcap)<<shift, noscan)
		overflow = uintptr(newcap) > (maxAlloc >> shift)
		newcap = int(capmem >> shift)
		capmem = uintptr(newcap) << shift
	default:
		lenmem = uintptr(oldLen) * et.size
		newlenmem = uintptr(newLen) * et.size
		capmem, overflow = mulUintptr(et.size, uintptr(newcap))
		capmem = roundupsize(capmem, noscan)
		newcap = int(capmem / et.size)
		capmem = uintptr(newcap) * et.size
	}

	if overflow || capmem > maxAlloc {
		panic(errorString("growslice: len out of range"))
	}

	p := mallocgc(capmem, et)
	memmove(p, oldPtr, lenmem)
	_ = newlenmem
	return slice{p, newLen, newcap}
}

// nextslicecap computes the next capacity, as the gc runtime does.
func nextslicecap(newLen, oldCap int) int {
	newcap := oldCap
	doublecap := newcap + newcap
	if newLen > doublecap {
		return newLen
	}
	const threshold = 256
	if oldCap < threshold {
		return doublecap
	}
	for {
		// Transition from growing 2x for small slices to growing 1.25x
		// for large slices.
		newcap += (newcap + 3*threshold) >> 2
		if uint(newcap) >= uint(newLen) {
			break
		}
	}
	if newcap <= 0 {
		return newLen
	}
	return newcap
}

// slicecopy implements copy(dst, src) for slices and returns the number of
// elements copied. Overlapping ranges are handled.
func slicecopy(dst unsafe.Pointer, dstLen int, src unsafe.Pointer, srcLen int, elemSize uintptr) int {
	n := srcLen
	if dstLen < n {
		n = dstLen
	}
	if n == 0 || elemSize == 0 {
		return n
	}
	size := uintptr(n) * elemSize
	if size == 1 {
		*(*byte)(dst) = *(*byte)(src)
	} else {
		memmove(dst, src, size)
	}
	return n
}

// typedslicecopy is slicecopy with the element type, for symmetry with gc.
func typedslicecopy(et *_type, dst unsafe.Pointer, dstLen int, src unsafe.Pointer, srcLen int) int {
	return slicecopy(dst, dstLen, src, srcLen, et.size)
}

// mulUintptr returns a * b and whether the product overflowed.
func mulUintptr(a, b uintptr) (uintptr, bool) {
	if a|b < 1<<(4*ptrSize) || a == 0 {
		return a * b, false
	}
	overflow := b > ^uintptr(0)/a
	return a * b, overflow
}

func isPowerOfTwo(x uintptr) bool {
	return x&(x-1) == 0
}

func trailingZeros64(x uint64) int {
	if x == 0 {
		return 64
	}
	n := 0
	for x&1 == 0 {
		x >>= 1
		n++
	}
	return n
}

// unsafeslice checks the arguments of unsafe.Slice(ptr, len).
func unsafeslice(et *_type, ptr unsafe.Pointer, len int) {
	if len < 0 {
		panicunsafeslicelen()
	}
	if et.size == 0 {
		if ptr == nil && len > 0 {
			panicunsafeslicenilptr()
		}
	}
	mem, overflow := mulUintptr(et.size, uintptr(len))
	if overflow || mem > -uintptr(ptr) {
		if ptr == nil {
			panicunsafeslicenilptr()
		}
		panicunsafeslicelen()
	}
}

// unsafestring checks the arguments of unsafe.String(ptr, len).
func unsafestring(ptr unsafe.Pointer, len int) {
	if len < 0 {
		panicunsafestringlen()
	}
	if uintptr(len) > -uintptr(ptr) {
		if ptr == nil {
			panicunsafestringnilptr()
		}
		panicunsafestringlen()
	}
}
