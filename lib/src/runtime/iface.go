// Interfaces: itab lookup and construction, type assertions, boxing.
// Pure file.

package runtime

import "unsafe"

// itabTable is an open addressing hash set of itabs built at run time,
// including negative results (flags&itabBad for a type that does not
// implement the interface). Compiler emitted itabs are not in it.
//
// Readers do not lock: the current table is published through an atomic
// pointer, a slot goes from nil to a fully built itab with an atomic store
// and never changes again, and growing builds a new table and publishes
// it. Writers serialize on itabLock.
type itabTableType struct {
	size    uintptr // a power of two
	count   uintptr
	entries unsafe.Pointer // size *itab slots
}

const itabBad = 1

var (
	itabLock  mutex
	itabTable unsafe.Pointer // *itabTableType, read atomically
)

func itabHashFunc(inter *interfacetype, typ *_type) uintptr {
	h := uintptr(unsafe.Pointer(inter))*0x9e3779b97f4a7c15 ^ uintptr(unsafe.Pointer(typ))
	return h ^ h>>29
}

func (t *itabTableType) slot(i uintptr) *unsafe.Pointer {
	return (*unsafe.Pointer)(unsafe.Add(t.entries, i*ptrSize))
}

// find looks (inter, typ) up without locking.
func (t *itabTableType) find(inter *interfacetype, typ *_type) *itab {
	mask := t.size - 1
	h := itabHashFunc(inter, typ) & mask
	for i := uintptr(1); ; i++ {
		m := (*itab)(atomicLoadp(t.slot(h)))
		if m == nil {
			return nil
		}
		if m.inter == inter && m.typ == typ {
			return m
		}
		h = (h + i) & mask
	}
}

// insert puts m into t, which has room. itabLock is held.
func (t *itabTableType) insert(m *itab) {
	mask := t.size - 1
	h := itabHashFunc(m.inter, m.typ) & mask
	for i := uintptr(1); ; i++ {
		if *t.slot(h) == nil {
			atomicStorep(t.slot(h), unsafe.Pointer(m))
			t.count++
			return
		}
		h = (h + i) & mask
	}
}

func newItabTable(size uintptr) *itabTableType {
	t := (*itabTableType)(mallocConservative(unsafe.Sizeof(itabTableType{})))
	t.size = size
	t.entries = mallocConservative(size * ptrSize)
	return t
}

// itabAdd adds m to the table, growing it first if needed. itabLock is
// held.
func itabAdd(m *itab) {
	t := (*itabTableType)(atomicLoadp(&itabTable))
	if t == nil {
		t = newItabTable(64)
		atomicStorep(&itabTable, unsafe.Pointer(t))
	}
	if (t.count+1)*4 > t.size*3 {
		// Keep the load factor under 3/4: copy into a table twice the
		// size and publish it. Readers of the old table still see valid
		// itabs.
		nt := newItabTable(t.size * 2)
		for i := uintptr(0); i < t.size; i++ {
			if e := *t.slot(i); e != nil {
				nt.insert((*itab)(e))
			}
		}
		atomicStorep(&itabTable, unsafe.Pointer(nt))
		t = nt
	}
	t.insert(m)
}

// itabFun returns the address of fun[i] of m.
func itabFun(m *itab, i int) *unsafe.Pointer {
	return (*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(&m.fun[0]), uintptr(i)*ptrSize))
}

// getitab returns the itab for (inter, typ). If typ does not implement inter
// it returns nil when canfail is set and panics with a *TypeAssertionError
// otherwise.
func getitab(inter *interfacetype, typ *_type, canfail bool) *itab {
	if inter.mcount == 0 {
		throw("internal error: getitab with an empty interface")
	}
	var m *itab
	if t := (*itabTableType)(atomicLoadp(&itabTable)); t != nil {
		m = t.find(inter, typ)
	}
	if m == nil {
		lock(&itabLock)
		if t := (*itabTableType)(atomicLoadp(&itabTable)); t != nil {
			m = t.find(inter, typ)
		}
		if m == nil {
			n := inter.mcount
			m = (*itab)(mallocConservative(unsafe.Sizeof(itab{}) + (n-1)*ptrSize))
			m.inter = inter
			m.typ = typ
			itabInit(m)
			itabAdd(m)
		}
		unlock(&itabLock)
	}
	if m.flags&itabBad == 0 {
		return m
	}
	if canfail {
		return nil
	}
	panic(&TypeAssertionError{concrete: typ, asserted: &inter.typ, missingMethod: itabMissing(inter, typ)})
}

// itabInit fills in m.fun. Both method lists are sorted by name, so one
// merge pass finds every method. A method matches on name and signature;
// its ifn may be nil when the compiler proved it is never called through
// an interface. On a missing method m is marked itabBad.
func itabInit(m *itab) {
	inter := m.inter
	typ := m.typ
	ni := int(inter.mcount)
	nt := 0
	if typ.uncommon != nil {
		nt = int(typ.uncommon.mcount)
	}
	j := 0
	for k := 0; k < ni; k++ {
		im := inter.methodAt(k)
		found := false
		for ; j < nt; j++ {
			tm := typ.methodAt(j)
			if tm.name == im.name {
				if tm.mtyp == im.ityp {
					*itabFun(m, k) = tm.ifn
					found = true
				}
				j++
				break
			}
			if tm.name > im.name {
				break
			}
		}
		if !found {
			m.flags |= itabBad
			return
		}
	}
}

// itabMissing returns the name of the first method of inter that typ lacks.
func itabMissing(inter *interfacetype, typ *_type) string {
	ni := int(inter.mcount)
	nt := 0
	if typ.uncommon != nil {
		nt = int(typ.uncommon.mcount)
	}
	for k := 0; k < ni; k++ {
		im := inter.methodAt(k)
		found := false
		for j := 0; j < nt; j++ {
			tm := typ.methodAt(j)
			if tm.name == im.name && tm.mtyp == im.ityp {
				found = true
				break
			}
		}
		if !found {
			return methodShortName(im.name)
		}
	}
	return ""
}

// methodShortName strips the package path from a qualified unexported
// method name.
func methodShortName(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return s[i+1:]
		}
		if s[i] == '/' {
			break
		}
	}
	return s
}

// assertE2I returns the itab for converting a value of dynamic type t to
// the interface inter, as in x.(I) and in conversions between interface
// types. It panics with a *TypeAssertionError if t is nil or does not
// implement inter.
func assertE2I(inter *interfacetype, t *_type) *itab {
	if t == nil {
		panic(&TypeAssertionError{asserted: &inter.typ})
	}
	return getitab(inter, t, false)
}

// assertE2I2 is assertE2I for the comma-ok form: it returns nil instead of
// panicking.
func assertE2I2(inter *interfacetype, t *_type) *itab {
	if t == nil {
		return nil
	}
	return getitab(inter, t, true)
}

// typeAssert checks whether a value of dynamic type t implements inter, for
// type switches with interface cases.
func typeAssert(inter *interfacetype, t *_type) bool {
	return t != nil && getitab(inter, t, true) != nil
}

// panicTypeAssert reports a failed assertion x.(T) to a concrete type T.
// have is the dynamic type of x (nil for a nil interface), want is T, and
// iface is the static type of x.
func panicTypeAssert(have, want, iface *_type) {
	panic(&TypeAssertionError{_interface: iface, concrete: have, asserted: want})
}

// Boxing of values into interface data words.

// convT returns a pointer to a new copy of the value of type t at v.
func convT(t *_type, v unsafe.Pointer) unsafe.Pointer {
	x := mallocgc(t.size, t)
	memmove(x, v, t.size)
	return x
}

// convTnoptr is convT for types without pointers.
func convTnoptr(t *_type, v unsafe.Pointer) unsafe.Pointer {
	x := mallocgc(t.size, nil)
	memmove(x, v, t.size)
	return x
}

// staticuint64s holds the values 0 to 255 so small integers and single
// bytes can be boxed without allocation.
var staticuint64s = [256]uint64{
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
	0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
	0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
	0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f,
	0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f,
	0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f,
	0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8e, 0x8f,
	0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9a, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f,
	0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf,
	0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf,
	0xc0, 0xc1, 0xc2, 0xc3, 0xc4, 0xc5, 0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xcb, 0xcc, 0xcd, 0xce, 0xcf,
	0xd0, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf,
	0xe0, 0xe1, 0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea, 0xeb, 0xec, 0xed, 0xee, 0xef,
	0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8, 0xf9, 0xfa, 0xfb, 0xfc, 0xfd, 0xfe, 0xff,
}

// convT16 boxes a 16 bit value.
func convT16(val uint16) unsafe.Pointer {
	if val < uint16(len(staticuint64s)) {
		return unsafe.Pointer(&staticuint64s[val])
	}
	x := mallocgc(2, nil)
	*(*uint16)(x) = val
	return x
}

// convT32 boxes a 32 bit value.
func convT32(val uint32) unsafe.Pointer {
	if val < uint32(len(staticuint64s)) {
		return unsafe.Pointer(&staticuint64s[val])
	}
	x := mallocgc(4, nil)
	*(*uint32)(x) = val
	return x
}

// convT64 boxes a 64 bit value.
func convT64(val uint64) unsafe.Pointer {
	if val < uint64(len(staticuint64s)) {
		return unsafe.Pointer(&staticuint64s[val])
	}
	x := mallocgc(8, nil)
	*(*uint64)(x) = val
	return x
}

// convTstring boxes a string.
func convTstring(val string) unsafe.Pointer {
	if val == "" {
		return unsafe.Pointer(&zeroWords)
	}
	x := mallocConservative(unsafe.Sizeof(val))
	*(*string)(x) = val
	return x
}

// convTslice boxes a slice.
func convTslice(val []byte) unsafe.Pointer {
	if (*slice)(unsafe.Pointer(&val)).array == nil {
		return unsafe.Pointer(&zeroWords)
	}
	x := mallocConservative(unsafe.Sizeof(val))
	*(*[]byte)(x) = val
	return x
}

// zeroWords is a zero value large enough for a boxed empty string or nil
// slice.
var zeroWords [4]uintptr
