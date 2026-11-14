// Types made at run time: SliceOf, MapOf, ArrayOf, ChanOf, FuncOf and
// StructOf.
//
// A result is the compiler's descriptor of the type when the binary has
// one (found in the compiler's type links, see canonicalType), so
// that values made with it match static type assertions. Otherwise reflect
// builds a descriptor and caches it, so each call with the same arguments
// returns the same Type. A built descriptor is identical to the static
// type for assignability, conversion and Set (reflect compares unnamed
// types structurally), but == on interfaces holding values of the two
// differs.
//
// Built types have no methods: FuncOf results cannot be called, and
// StructOf does not promote methods of embedded fields.

package reflect

import (
	"internal/rtabi"
	"strconv"
	"sync"
	"unicode"
	"unicode/utf8"
	"unsafe"
)

const ptrSize = unsafe.Sizeof(uintptr(0))

// madeTypes caches built descriptors by a key describing the type.
var madeTypes sync.Map // map[any]*rtabi.Type

func madeType(key any, build func() *rtabi.Type) *rtabi.Type {
	if t, ok := madeTypes.Load(key); ok {
		return t.(*rtabi.Type)
	}
	t := build()
	if c := canonicalType(t); c != nil {
		t = c
	}
	got, _ := madeTypes.LoadOrStore(key, t)
	return got.(*rtabi.Type)
}

// canonicalType returns the compiler's descriptor of the type that t
// describes, or nil.
func canonicalType(t *rtabi.Type) *rtabi.Type {
	return typelinksFind(t.Str, func(c *rtabi.Type) bool {
		return c.Kind_ == t.Kind_ && !c.HasName() && haveIdenticalUnderlyingType(c, t, true)
	})
}

// canonicalPtrTo returns the compiler's descriptor of *t, or nil.
func canonicalPtrTo(t *rtabi.Type) *rtabi.Type {
	return typelinksFind("*"+t.Str, func(c *rtabi.Type) bool {
		return c.Kind_ == rtabi.Pointer && (*rtabi.PtrType)(unsafe.Pointer(c)).Elem == t
	})
}

//go:linkname rt_typelinks rt_typelinks
//go:noescape
func rt_typelinks(n *int) unsafe.Pointer

// The descriptors of unnamed composite types the compiler emitted (section
// marmot_tlinks, found by rt_typelinks), indexed by type string: an open
// addressing table of uint32 positions into the section (+1, 0 empty),
// one slot per distinct descriptor at load 1/2 or less. A descriptor the
// section lists more than once is indexed once. About 8 bytes per type,
// against some 60 for a map of slices.
var (
	typelinksOnce sync.Once
	tlTypes       []*rtabi.Type
	tlSlots       []uint32
)

func strHash(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 16777619
	}
	return h
}

func typelinksBuild() {
	var n int
	p := rt_typelinks(&n)
	if p == nil || n == 0 {
		return
	}
	tlTypes = unsafe.Slice((**rtabi.Type)(p), n)
	slots := make([]uint32, 1024)
	used := 0
	insert := func(slots []uint32, i int) bool {
		t := tlTypes[i]
		mask := uint32(len(slots) - 1)
		for k := strHash(t.Str) & mask; ; k = (k + 1) & mask {
			j := slots[k]
			if j == 0 {
				slots[k] = uint32(i + 1)
				return true
			}
			if tlTypes[j-1] == t {
				return false // already indexed
			}
		}
	}
	for i, t := range tlTypes {
		if t == nil {
			continue
		}
		if (used+1)*2 > len(slots) {
			bigger := make([]uint32, len(slots)*2)
			for _, j := range slots {
				if j != 0 {
					insert(bigger, int(j-1))
				}
			}
			slots = bigger
		}
		if insert(slots, i) {
			used++
		}
	}
	tlSlots = slots
}

// typelinksFind returns the first descriptor with type string s for which
// ok holds, or nil.
func typelinksFind(s string, ok func(*rtabi.Type) bool) *rtabi.Type {
	typelinksOnce.Do(typelinksBuild)
	if len(tlSlots) == 0 {
		return nil
	}
	mask := uint32(len(tlSlots) - 1)
	for k := strHash(s) & mask; ; k = (k + 1) & mask {
		j := tlSlots[k]
		if j == 0 {
			return nil
		}
		if t := tlTypes[j-1]; t.Str == s && ok(t) {
			return t
		}
	}
}

// gcBits is a pointer bitmap under construction, one bit per word.
type gcBits []byte

func (b gcBits) set(word uintptr) { b[word/8] |= 1 << (word % 8) }

// copyFrom ors in the bitmap of t at byte offset off.
func (b gcBits) copyFrom(t *rtabi.Type, off uintptr) bool {
	if t.PtrBytes == 0 {
		return true
	}
	if t.GCData == nil {
		return false // t is scanned conservatively
	}
	src := unsafe.Slice(t.GCData, (t.Size_/ptrSize+7)/8)
	for w := uintptr(0); w < t.PtrBytes/ptrSize; w++ {
		if src[w/8]&(1<<(w%8)) != 0 {
			b.set(off/ptrSize + w)
		}
	}
	return true
}

// maxPreciseWords bounds the bitmaps reflect builds; larger types are
// scanned conservatively.
const maxPreciseWords = 1 << 20

func newGCBits(size uintptr) gcBits {
	return make(gcBits, (size/ptrSize+7)/8)
}

func pointerType(size uintptr, kind rtabi.Kind, str string, tflag uint8) rtabi.Type {
	return rtabi.Type{
		Size_: size, PtrBytes: ptrSize, GCData: &pointerGCData, Str: str,
		Kind_: kind, Align_: uint8(ptrSize), FieldAlign: uint8(ptrSize), TFlag: tflag,
	}
}

// SliceOf returns the slice type with element type t.
func SliceOf(t Type) Type {
	e := t.common()
	type key struct{ elem *rtabi.Type }
	return toType(madeType(key{e}, func() *rtabi.Type {
		st := new(rtabi.SliceType)
		st.Type = pointerType(3*ptrSize, rtabi.Slice, "[]"+e.Str, 0)
		st.Elem = e
		return &st.Type
	}))
}

// MapOf returns the map type with the given key and element types. It
// panics if the key type is not comparable.
func MapOf(key, elem Type) Type {
	k, e := key.common(), elem.common()
	if !k.Comparable() {
		panic("reflect.MapOf: invalid key type " + k.Str)
	}
	type mkey struct{ k, e *rtabi.Type }
	return toType(madeType(mkey{k, e}, func() *rtabi.Type {
		mt := new(rtabi.MapType)
		mt.Type = pointerType(ptrSize, rtabi.Map, "map["+k.Str+"]"+e.Str, rtabi.TFlagDirectIface)
		mt.Key, mt.Elem = k, e
		return &mt.Type
	}))
}

// ChanOf returns the channel type with the given direction and element
// type.
func ChanOf(dir ChanDir, t Type) Type {
	e := t.common()
	var s string
	switch dir {
	case SendDir:
		s = "chan<- " + e.Str
	case RecvDir:
		s = "<-chan " + e.Str
	case BothDir:
		if e.Kind_ == rtabi.Chan && (*rtabi.ChanType)(unsafe.Pointer(e)).Dir == uintptr(RecvDir) {
			s = "chan (" + e.Str + ")"
		} else {
			s = "chan " + e.Str
		}
	default:
		panic("reflect.ChanOf: invalid dir")
	}
	if e.Size_ >= 1<<16 {
		panic("reflect.ChanOf: element size too large")
	}
	type ckey struct {
		dir  ChanDir
		elem *rtabi.Type
	}
	return toType(madeType(ckey{dir, e}, func() *rtabi.Type {
		ct := new(rtabi.ChanType)
		ct.Type = pointerType(ptrSize, rtabi.Chan, s, rtabi.TFlagComparable|rtabi.TFlagRegularMemory|rtabi.TFlagDirectIface)
		ct.Elem, ct.Dir = e, uintptr(dir)
		return &ct.Type
	}))
}

// ArrayOf returns the array type with the given length and element type.
func ArrayOf(length int, elem Type) Type {
	e := elem.common()
	if length < 0 {
		panic("reflect: negative length passed to ArrayOf")
	}
	if e.Size_ > 0 && uintptr(length) > ^uintptr(0)/e.Size_ {
		panic("reflect.ArrayOf: array size would exceed virtual address space")
	}
	type akey struct {
		n    int
		elem *rtabi.Type
	}
	return toType(madeType(akey{length, e}, func() *rtabi.Type {
		n := uintptr(length)
		at := new(rtabi.ArrayType)
		at.Size_ = n * e.Size_
		at.Align_, at.FieldAlign = e.Align_, e.FieldAlign
		at.Kind_ = rtabi.Array
		at.Str = "[" + strconv.Itoa(length) + "]" + e.Str
		at.TFlag = e.TFlag & (rtabi.TFlagComparable | rtabi.TFlagRegularMemory)
		if n == 1 {
			at.TFlag |= e.TFlag & rtabi.TFlagDirectIface
		}
		if n > 0 && e.PtrBytes != 0 {
			at.PtrBytes = (n-1)*e.Size_ + e.PtrBytes
			if at.Size_/ptrSize <= maxPreciseWords && e.GCData != nil {
				bits := newGCBits(at.Size_)
				for i := uintptr(0); i < n; i++ {
					bits.copyFrom(e, i*e.Size_)
				}
				at.GCData = &bits[0]
			}
		}
		at.Elem, at.Len = e, n
		return &at.Type
	}))
}

// FuncOf returns the function type with the given argument and result
// types. Functions of a type built by FuncOf cannot be called through
// reflect.
func FuncOf(in, out []Type, variadic bool) Type {
	if variadic && (len(in) == 0 || in[len(in)-1].Kind() != Slice) {
		panic("reflect.FuncOf: last arg of variadic func must be slice")
	}
	if len(in)+len(out) > 1<<16-1 {
		panic("reflect.FuncOf: too many arguments")
	}
	params := make([]*rtabi.Type, 0, len(in)+len(out))
	for _, t := range in {
		params = append(params, t.common())
	}
	for _, t := range out {
		params = append(params, t.common())
	}
	s := "func("
	for i, t := range in {
		if i > 0 {
			s += ", "
		}
		if variadic && i == len(in)-1 {
			s += "..." + t.common().Elem().Str
		} else {
			s += t.common().Str
		}
	}
	s += ")"
	switch {
	case len(out) == 1:
		s += " " + out[0].common().Str
	case len(out) > 1:
		s += " ("
		for i, t := range out {
			if i > 0 {
				s += ", "
			}
			s += t.common().Str
		}
		s += ")"
	}
	// The string spells out every parameter type, so it identifies the
	// signature up to distinct types with the same name, which the
	// cached entry is checked against.
	type fkey struct{ s string }
	build := func() *rtabi.Type {
		ft := new(rtabi.FuncType)
		ft.Type = pointerType(ptrSize, rtabi.Func, s, rtabi.TFlagDirectIface)
		if len(params) > 0 {
			ft.Params = &params[0]
		}
		ft.InCount, ft.OutCount = uint16(len(in)), uint16(len(out))
		if variadic {
			ft.Variadic = 1
		}
		return &ft.Type
	}
	t := madeType(fkey{s}, build)
	if f := (*rtabi.FuncType)(unsafe.Pointer(t)); !sameParams(f, params) {
		t = build()
	}
	return toType(t)
}

func sameParams(f *rtabi.FuncType, params []*rtabi.Type) bool {
	if int(f.InCount)+int(f.OutCount) != len(params) {
		return false
	}
	for i, p := range params {
		if f.Param(i) != p {
			return false
		}
	}
	return true
}

// StructOf returns the struct type containing fields. Embedded fields do
// not promote methods.
func StructOf(fields []StructField) Type {
	sfs := make([]rtabi.StructField, len(fields))
	var (
		off      uintptr
		align    uint8 = 1
		tflag          = uint8(rtabi.TFlagComparable | rtabi.TFlagRegularMemory)
		ptrBytes uintptr
		pkgpath  string
		seen     = map[string]bool{}
	)
	s := "struct {"
	for i, f := range fields {
		if f.Name == "" {
			panic("reflect.StructOf: field " + strconv.Itoa(i) + " has no name")
		}
		if !isValidFieldName(f.Name) {
			panic("reflect.StructOf: field " + strconv.Itoa(i) + " has invalid name")
		}
		if f.Type == nil {
			panic("reflect.StructOf: field " + strconv.Itoa(i) + " has no type")
		}
		if seen[f.Name] && f.Name != "_" {
			panic("reflect.StructOf: duplicate field " + f.Name)
		}
		seen[f.Name] = true
		if f.PkgPath != "" {
			if pkgpath == "" {
				pkgpath = f.PkgPath
			} else if pkgpath != f.PkgPath {
				panic("reflect.StructOf: fields with different PkgPath " + pkgpath + " and " + f.PkgPath)
			}
		} else if r, _ := utf8.DecodeRuneInString(f.Name); !unicode.IsUpper(r) {
			panic("reflect.StructOf: field \"" + f.Name + "\" is unexported but missing PkgPath")
		}
		ft := f.Type.common()
		a := uintptr(ft.FieldAlign)
		if a == 0 {
			a = 1
		}
		if aligned := (off + a - 1) &^ (a - 1); aligned != off {
			tflag &^= rtabi.TFlagRegularMemory // padding
			off = aligned
		}
		if ft.FieldAlign > align {
			align = ft.FieldAlign
		}
		tflag &= ft.TFlag&(rtabi.TFlagComparable|rtabi.TFlagRegularMemory) | ^uint8(rtabi.TFlagComparable|rtabi.TFlagRegularMemory)
		if ft.PtrBytes != 0 {
			ptrBytes = off + ft.PtrBytes
		}
		embed := uintptr(0)
		if f.Anonymous {
			embed = 1
		}
		sfs[i] = rtabi.StructField{Name: f.Name, Tag: string(f.Tag), Typ: ft, OffsetEmbed: off<<1 | embed}
		if i > 0 {
			s += ";"
		}
		if f.Anonymous {
			s += " " + ft.Str
		} else {
			s += " " + f.Name + " " + ft.Str
		}
		if f.Tag != "" {
			s += " " + strconv.Quote(string(f.Tag))
		}
		if ft.Size_ > ^uintptr(0)-off {
			panic("reflect.StructOf: struct size would exceed virtual address space")
		}
		off += ft.Size_
	}
	if len(fields) > 0 && off > 0 && fields[len(fields)-1].Type.Size() == 0 {
		// A final zero size field gets a byte so that its address does
		// not point past the struct (as the gc compiler lays it out).
		off++
	}
	size := (off + uintptr(align) - 1) &^ (uintptr(align) - 1)
	if size != off {
		tflag &^= rtabi.TFlagRegularMemory
	}
	if len(fields) > 0 {
		s += " "
	}
	s += "}"
	if len(fields) == 1 && size == ptrSize && sfs[0].Typ.TFlag&rtabi.TFlagDirectIface != 0 {
		tflag |= rtabi.TFlagDirectIface
	}

	type skey struct{ s string }
	build := func() *rtabi.Type {
		st := new(rtabi.StructType)
		st.Size_, st.PtrBytes = size, ptrBytes
		st.Align_, st.FieldAlign = align, align
		st.Kind_ = rtabi.Struct
		st.Str = s
		st.TFlag = tflag
		if ptrBytes != 0 && size/ptrSize <= maxPreciseWords {
			bits := newGCBits(size)
			ok := true
			for i := range sfs {
				ok = ok && bits.copyFrom(sfs[i].Typ, sfs[i].OffsetEmbed>>1)
			}
			if ok {
				st.GCData = &bits[0]
			}
		}
		st.PkgPath = pkgpath
		if len(sfs) > 0 {
			st.Fields = &sfs[0]
		}
		st.NFields = uintptr(len(sfs))
		return &st.Type
	}
	t := madeType(skey{s + "\x00" + pkgpath}, build)
	if !sameFields((*rtabi.StructType)(unsafe.Pointer(t)), sfs) {
		t = build()
	}
	return toType(t)
}

func sameFields(st *rtabi.StructType, sfs []rtabi.StructField) bool {
	if int(st.NFields) != len(sfs) {
		return false
	}
	fs := unsafe.Slice(st.Fields, st.NFields)
	for i := range sfs {
		if fs[i] != sfs[i] {
			return false
		}
	}
	return true
}

func isValidFieldName(name string) bool {
	for i, c := range name {
		isLetter := c == '_' || unicode.IsLetter(c)
		if i == 0 && !isLetter {
			return false
		}
		if !(isLetter || unicode.IsDigit(c)) {
			return false
		}
	}
	return len(name) > 0
}
