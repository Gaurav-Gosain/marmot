// Hashing and equality driven by type descriptors. Used by maps, interface
// comparison and the compiler for == on values whose type it does not
// compare inline. Pure file.

package runtime

import "unsafe"

const (
	m1 = 0xa0761d6478bd642f
	m2 = 0xe7037ed1a0b428db
	m3 = 0x8ebc6af09c88c6e3
	m4 = 0x589965cc75374cc3
	m5 = 0x1d8e4e27c47d124f
)

// hashkey is the process-wide seed mixed into every hash, set at startup.
var hashkey uintptr = 0x2d358dccaa6c78a5

func mix(a, b uintptr) uintptr {
	hi, lo := mul64(uint64(a), uint64(b))
	return uintptr(hi ^ lo)
}

func r4(p unsafe.Pointer) uintptr {
	q := (*[4]byte)(p)
	return uintptr(uint32(q[0]) | uint32(q[1])<<8 | uint32(q[2])<<16 | uint32(q[3])<<24)
}

func r8(p unsafe.Pointer) uintptr {
	q := (*[8]byte)(p)
	return uintptr(uint64(q[0]) | uint64(q[1])<<8 | uint64(q[2])<<16 | uint64(q[3])<<24 |
		uint64(q[4])<<32 | uint64(q[5])<<40 | uint64(q[6])<<48 | uint64(q[7])<<56)
}

// memhash hashes s bytes at p (wyhash).
func memhash(p unsafe.Pointer, seed, s uintptr) uintptr {
	var a, b uintptr
	seed ^= hashkey ^ m1
	switch {
	case s == 0:
		return seed
	case s < 4:
		q := (*[3]byte)(p)
		a = uintptr(q[0])
		a |= uintptr(q[s>>1]) << 8
		a |= uintptr(q[s-1]) << 16
	case s == 4:
		a = r4(p)
		b = a
	case s < 8:
		a = r4(p)
		b = r4(unsafe.Add(p, s-4))
	case s == 8:
		a = r8(p)
		b = a
	case s <= 16:
		a = r8(p)
		b = r8(unsafe.Add(p, s-8))
	default:
		l := s
		if l > 48 {
			seed1 := seed
			seed2 := seed
			for ; l > 48; l -= 48 {
				seed = mix(r8(p)^m2, r8(unsafe.Add(p, 8))^seed)
				seed1 = mix(r8(unsafe.Add(p, 16))^m3, r8(unsafe.Add(p, 24))^seed1)
				seed2 = mix(r8(unsafe.Add(p, 32))^m4, r8(unsafe.Add(p, 40))^seed2)
				p = unsafe.Add(p, 48)
			}
			seed ^= seed1 ^ seed2
		}
		for ; l > 16; l -= 16 {
			seed = mix(r8(p)^m2, r8(unsafe.Add(p, 8))^seed)
			p = unsafe.Add(p, 16)
		}
		a = r8(unsafe.Add(p, l-16))
		b = r8(unsafe.Add(p, l-8))
	}
	return mix(m5^s, mix(a^m2, b^seed))
}

func memhash32(p unsafe.Pointer, seed uintptr) uintptr {
	a := r4(p)
	return mix(m5^4, mix(a^m2, a^seed^hashkey^m1))
}

func memhash64(p unsafe.Pointer, seed uintptr) uintptr {
	a := r8(p)
	return mix(m5^8, mix(a^m2, a^seed^hashkey^m1))
}

func strhash(p unsafe.Pointer, seed uintptr) uintptr {
	s := (*stringStruct)(p)
	return memhash(s.str, seed, uintptr(s.len))
}

func f32hash(p unsafe.Pointer, seed uintptr) uintptr {
	f := *(*float32)(p)
	switch {
	case f == 0:
		return memhash32(unsafe.Pointer(&zeroWord), seed) // +0, -0
	case f != f:
		return uintptr(cheaprand64()) ^ seed // any kind of NaN
	}
	return memhash32(p, seed)
}

func f64hash(p unsafe.Pointer, seed uintptr) uintptr {
	f := *(*float64)(p)
	switch {
	case f == 0:
		return memhash64(unsafe.Pointer(&zeroWord), seed) // +0, -0
	case f != f:
		return uintptr(cheaprand64()) ^ seed // any kind of NaN
	}
	return memhash64(p, seed)
}

var zeroWord uint64

// nilinterhash hashes an interface{} value stored at p.
func nilinterhash(p unsafe.Pointer, seed uintptr) uintptr {
	e := (*eface)(p)
	t := e.typ
	if t == nil {
		return seed
	}
	return hashOfDynamic(t, e.data, seed)
}

// interhash hashes a non-empty interface value stored at p.
func interhash(p unsafe.Pointer, seed uintptr) uintptr {
	i := (*iface)(p)
	if i.tab == nil {
		return seed
	}
	return hashOfDynamic(i.tab.typ, i.data, seed)
}

func hashOfDynamic(t *_type, data unsafe.Pointer, seed uintptr) uintptr {
	if t.tflag&tflagComparable == 0 {
		panic(errorString("hash of unhashable type " + t.str))
	}
	if t.tflag&tflagDirectIface != 0 {
		return mix(uintptr(unsafe.Pointer(t)), typehash(t, unsafe.Pointer(&data), seed))
	}
	return mix(uintptr(unsafe.Pointer(t)), typehash(t, data, seed))
}

// typehash hashes the value of type t at p. Types that are not comparable
// panic, as in the gc runtime.
func typehash(t *_type, p unsafe.Pointer, seed uintptr) uintptr {
	if t.tflag&tflagRegularMemory != 0 {
		switch t.size {
		case 4:
			return memhash32(p, seed)
		case 8:
			return memhash64(p, seed)
		}
		return memhash(p, seed, t.size)
	}
	switch t.kind {
	case kindFloat32:
		return f32hash(p, seed)
	case kindFloat64:
		return f64hash(p, seed)
	case kindComplex64:
		return f32hash(unsafe.Add(p, 4), f32hash(p, seed))
	case kindComplex128:
		return f64hash(unsafe.Add(p, 8), f64hash(p, seed))
	case kindString:
		return strhash(p, seed)
	case kindInterface:
		if (*interfacetype)(unsafe.Pointer(t)).mcount == 0 {
			return nilinterhash(p, seed)
		}
		return interhash(p, seed)
	case kindArray:
		a := (*arraytype)(unsafe.Pointer(t))
		h := seed
		for i := uintptr(0); i < a.len; i++ {
			h = typehash(a.elem, unsafe.Add(p, i*a.elem.size), h)
		}
		return h
	case kindStruct:
		s := (*structtype)(unsafe.Pointer(t))
		h := seed
		for i := 0; i < int(s.nfields); i++ {
			f := s.fieldAt(i)
			if f.name == "_" {
				continue
			}
			h = typehash(f.typ, unsafe.Add(p, f.offset()), h)
		}
		return h
	case kindBool, kindInt, kindInt8, kindInt16, kindInt32, kindInt64,
		kindUint, kindUint8, kindUint16, kindUint32, kindUint64, kindUintptr,
		kindPointer, kindChan, kindUnsafePointer:
		// Plain memory even if the compiler did not set the flag.
		return memhash(p, seed, t.size)
	}
	panic(errorString("hash of unhashable type " + t.str))
}

// typeequal reports whether the values of type t at p and q are equal.
// Types that are not comparable panic.
func typeequal(t *_type, p, q unsafe.Pointer) bool {
	if t.tflag&tflagRegularMemory != 0 {
		return memequal(p, q, t.size)
	}
	switch t.kind {
	case kindFloat32:
		return *(*float32)(p) == *(*float32)(q)
	case kindFloat64:
		return *(*float64)(p) == *(*float64)(q)
	case kindComplex64:
		return *(*complex64)(p) == *(*complex64)(q)
	case kindComplex128:
		return *(*complex128)(p) == *(*complex128)(q)
	case kindString:
		return *(*string)(p) == *(*string)(q)
	case kindInterface:
		if (*interfacetype)(unsafe.Pointer(t)).mcount == 0 {
			x := (*eface)(p)
			y := (*eface)(q)
			return x.typ == y.typ && efaceeqT(x.typ, x.data, y.data)
		}
		return ifaceeq(*(*iface)(p), *(*iface)(q))
	case kindArray:
		a := (*arraytype)(unsafe.Pointer(t))
		for i := uintptr(0); i < a.len; i++ {
			off := i * a.elem.size
			if !typeequal(a.elem, unsafe.Add(p, off), unsafe.Add(q, off)) {
				return false
			}
		}
		return true
	case kindStruct:
		s := (*structtype)(unsafe.Pointer(t))
		for i := 0; i < int(s.nfields); i++ {
			f := s.fieldAt(i)
			if f.name == "_" {
				continue
			}
			off := f.offset()
			if !typeequal(f.typ, unsafe.Add(p, off), unsafe.Add(q, off)) {
				return false
			}
		}
		return true
	case kindBool, kindInt, kindInt8, kindInt16, kindInt32, kindInt64,
		kindUint, kindUint8, kindUint16, kindUint32, kindUint64, kindUintptr,
		kindPointer, kindChan, kindUnsafePointer:
		return memequal(p, q, t.size)
	}
	panic(errorString("comparing uncomparable type " + t.str))
}

// efaceeqT compares the data words of two interface{} values whose type
// words are both t (the caller has checked that they are equal and
// non-nil).
func efaceeqT(t *_type, x, y unsafe.Pointer) bool {
	if t == nil {
		return true
	}
	if t.tflag&tflagComparable == 0 {
		panic(errorString("comparing uncomparable type " + t.str))
	}
	if t.tflag&tflagDirectIface != 0 {
		return x == y
	}
	return typeequal(t, x, y)
}

// ifaceeqT is efaceeqT for non-empty interfaces whose itab words are both tab.
func ifaceeqT(tab *itab, x, y unsafe.Pointer) bool {
	if tab == nil {
		return true
	}
	return efaceeqT(tab.typ, x, y)
}

// efaceeq implements x == y for two interface{} values.
func efaceeq(x, y eface) bool {
	if x.typ != y.typ {
		return false
	}
	return efaceeqT(x.typ, x.data, y.data)
}

// ifaceeq implements x == y for two non-empty interface values of the same
// interface type.
//
// The itab words may differ for equal values: the compiler emits itabs for
// static conversions and the runtime builds its own in assertE2I, so the
// dynamic types are compared, not the itab pointers.
func ifaceeq(x, y iface) bool {
	if x.tab == y.tab {
		return ifaceeqT(x.tab, x.data, y.data)
	}
	if x.tab == nil || y.tab == nil || x.tab.typ != y.tab.typ {
		return false
	}
	return efaceeqT(x.tab.typ, x.data, y.data)
}
