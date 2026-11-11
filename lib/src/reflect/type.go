// Package reflect implements run-time reflection.
//
// marmot overlay: the reflect API on marmot's type descriptors
// (internal/rtabi, mirrored from runtime/type.go). Supported: type
// inspection, values of every kind, setters, conversions, maps, slices,
// channels, DeepEqual, and Value.Call and Value.Method through the
// compiler's call adapters (functype.call, method.tfn). Not supported:
// MakeFunc, Method(i).Func, and creating new types (StructOf, FuncOf,
// ...), which panic.
package reflect

import (
	"internal/rtabi"
	"iter"
	"strconv"
	"sync"
	"unicode"
	"unicode/utf8"
	"unsafe"
)

// A Kind represents the specific kind of type that a Type represents.
type Kind uint

const (
	Invalid Kind = iota
	Bool
	Int
	Int8
	Int16
	Int32
	Int64
	Uint
	Uint8
	Uint16
	Uint32
	Uint64
	Uintptr
	Float32
	Float64
	Complex64
	Complex128
	Array
	Chan
	Func
	Interface
	Map
	Pointer
	Slice
	String
	Struct
	UnsafePointer
)

// Ptr is the old name for the Pointer kind.
const Ptr = Pointer

var kindNames = []string{
	Invalid:       "invalid",
	Bool:          "bool",
	Int:           "int",
	Int8:          "int8",
	Int16:         "int16",
	Int32:         "int32",
	Int64:         "int64",
	Uint:          "uint",
	Uint8:         "uint8",
	Uint16:        "uint16",
	Uint32:        "uint32",
	Uint64:        "uint64",
	Uintptr:       "uintptr",
	Float32:       "float32",
	Float64:       "float64",
	Complex64:     "complex64",
	Complex128:    "complex128",
	Array:         "array",
	Chan:          "chan",
	Func:          "func",
	Interface:     "interface",
	Map:           "map",
	Pointer:       "ptr",
	Slice:         "slice",
	String:        "string",
	Struct:        "struct",
	UnsafePointer: "unsafe.Pointer",
}

// String returns the name of k.
func (k Kind) String() string {
	if uint(k) < uint(len(kindNames)) {
		return kindNames[uint(k)]
	}
	return "kind" + strconv.Itoa(int(k))
}

// ChanDir represents a channel type's direction.
type ChanDir int

const (
	RecvDir ChanDir = 1 << iota
	SendDir
	BothDir = RecvDir | SendDir
)

func (d ChanDir) String() string {
	switch d {
	case SendDir:
		return "chan<-"
	case RecvDir:
		return "<-chan"
	case BothDir:
		return "chan"
	}
	return "ChanDir" + strconv.Itoa(int(d))
}

// Type is the representation of a Go type.
type Type interface {
	Align() int
	FieldAlign() int
	Method(int) Method
	Methods() iter.Seq[Method]
	MethodByName(string) (Method, bool)
	NumMethod() int
	Name() string
	PkgPath() string
	Size() uintptr
	String() string
	Kind() Kind
	Implements(u Type) bool
	AssignableTo(u Type) bool
	ConvertibleTo(u Type) bool
	Comparable() bool
	Bits() int
	ChanDir() ChanDir
	IsVariadic() bool
	Elem() Type
	Field(i int) StructField
	Fields() iter.Seq[StructField]
	FieldByIndex(index []int) StructField
	FieldByName(name string) (StructField, bool)
	FieldByNameFunc(match func(string) bool) (StructField, bool)
	In(i int) Type
	Ins() iter.Seq[Type]
	Key() Type
	Len() int
	NumField() int
	NumIn() int
	NumOut() int
	Out(i int) Type
	Outs() iter.Seq[Type]
	OverflowComplex(x complex128) bool
	OverflowFloat(x float64) bool
	OverflowInt(x int64) bool
	OverflowUint(x uint64) bool
	CanSeq() bool
	CanSeq2() bool
	common() *rtabi.Type
	uncommon() *rtabi.UncommonType
}

// rtype is the implementation of Type: a pointer to a descriptor.
type rtype struct {
	t rtabi.Type
}

func toRType(t *rtabi.Type) *rtype { return (*rtype)(unsafe.Pointer(t)) }

func toType(t *rtabi.Type) Type {
	if t == nil {
		return nil
	}
	return toRType(t)
}

func (t *rtype) common() *rtabi.Type           { return &t.t }
func (t *rtype) uncommon() *rtabi.UncommonType { return t.t.Uncommon_ }
func (t *rtype) Align() int                    { return int(t.t.Align_) }
func (t *rtype) FieldAlign() int               { return int(t.t.FieldAlign) }
func (t *rtype) Name() string                  { return t.t.Name() }
func (t *rtype) PkgPath() string               { return t.t.PkgPath() }
func (t *rtype) Size() uintptr                 { return t.t.Size_ }
func (t *rtype) String() string                { return t.t.Str }
func (t *rtype) Kind() Kind                    { return Kind(t.t.Kind_) }
func (t *rtype) Comparable() bool              { return t.t.Comparable() }

func (t *rtype) Bits() int {
	k := t.Kind()
	if k < Int || k > Complex128 {
		panic("reflect: Bits of non-arithmetic Type " + t.String())
	}
	return int(t.t.Size_) * 8
}

func (t *rtype) ChanDir() ChanDir {
	if t.Kind() != Chan {
		panic("reflect: ChanDir of non-chan type " + t.String())
	}
	return ChanDir((*rtabi.ChanType)(unsafe.Pointer(t)).Dir)
}

func (t *rtype) IsVariadic() bool {
	if t.Kind() != Func {
		panic("reflect: IsVariadic of non-func type " + t.String())
	}
	return (*rtabi.FuncType)(unsafe.Pointer(t)).Variadic != 0
}

func (t *rtype) Elem() Type {
	e := t.t.Elem()
	if e == nil {
		panic("reflect: Elem of invalid type " + t.String())
	}
	return toType(e)
}

func (t *rtype) Key() Type {
	if t.Kind() != Map {
		panic("reflect: Key of non-map type " + t.String())
	}
	return toType(t.t.Key())
}

func (t *rtype) Len() int {
	if t.Kind() != Array {
		panic("reflect: Len of non-array type " + t.String())
	}
	return t.t.Len()
}

func (t *rtype) structType() *rtabi.StructType {
	if t.Kind() != Struct {
		panic("reflect: Field of non-struct type " + t.String())
	}
	return (*rtabi.StructType)(unsafe.Pointer(t))
}

func (t *rtype) NumField() int {
	if t.Kind() != Struct {
		panic("reflect: NumField of non-struct type " + t.String())
	}
	return int(t.structType().NFields)
}

func (t *rtype) funcType() *rtabi.FuncType {
	if t.Kind() != Func {
		panic("reflect: In of non-func type " + t.String())
	}
	return (*rtabi.FuncType)(unsafe.Pointer(t))
}

func (t *rtype) NumIn() int  { return int(t.funcType().InCount) }
func (t *rtype) NumOut() int { return int(t.funcType().OutCount) }

func (t *rtype) In(i int) Type {
	ft := t.funcType()
	if uint(i) >= uint(ft.InCount) {
		panic("reflect: Function index out of range")
	}
	return toType(ft.Param(i))
}

func (t *rtype) Out(i int) Type {
	ft := t.funcType()
	if uint(i) >= uint(ft.OutCount) {
		panic("reflect: Function index out of range")
	}
	return toType(ft.Param(int(ft.InCount) + i))
}

// NumMethod returns the number of exported methods (all methods for
// interface types).
func (t *rtype) NumMethod() int { return t.t.NumMethod() }

// Method describes a single method.
type Method struct {
	Name    string
	PkgPath string
	Type    Type  // method type (marmot: without the receiver)
	Func    Value // marmot: always the zero Value
	Index   int
}

// IsExported reports whether the method is exported.
func (m Method) IsExported() bool { return m.PkgPath == "" }

func (t *rtype) Method(i int) Method {
	if t.Kind() == Interface {
		it := (*rtabi.InterfaceType)(unsafe.Pointer(t))
		if uint(i) >= uint(it.Mcount) {
			panic("reflect: Method index out of range")
		}
		im := it.MethodAt(i)
		m := Method{Name: rtabi.ShortName(im.Name), Type: toType(im.Ityp), Index: i}
		if !rtabi.IsExported(im.Name) {
			m.PkgPath = it.PkgPath
		}
		return m
	}
	u := t.t.Uncommon_
	if u == nil || uint(i) >= uint(u.Xcount) {
		panic("reflect: Method index out of range")
	}
	tm := u.MethodAt(i)
	return Method{Name: tm.Name, Type: toType(tm.Mtyp), Index: i}
}

func (t *rtype) MethodByName(name string) (Method, bool) {
	n := t.NumMethod()
	for i := 0; i < n; i++ {
		m := t.Method(i)
		if m.Name == name {
			return m, true
		}
	}
	return Method{}, false
}

// A StructField describes a single field in a struct.
type StructField struct {
	Name      string
	PkgPath   string
	Type      Type
	Tag       StructTag
	Offset    uintptr
	Index     []int
	Anonymous bool
}

// IsExported reports whether the field is exported.
func (f StructField) IsExported() bool { return f.PkgPath == "" }

func (t *rtype) Field(i int) StructField {
	st := t.structType()
	if uint(i) >= uint(st.NFields) {
		panic("reflect: Field index out of bounds")
	}
	return structField(st, i)
}

func structField(st *rtabi.StructType, i int) StructField {
	p := st.Field(i)
	f := StructField{
		Name:      p.Name,
		Type:      toType(p.Typ),
		Tag:       StructTag(p.Tag),
		Offset:    p.Offset(),
		Index:     []int{i},
		Anonymous: p.Embedded(),
	}
	if !isExportedName(p.Name) {
		f.PkgPath = st.PkgPath
	}
	return f
}

func isExportedName(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r)
}

func (t *rtype) FieldByIndex(index []int) StructField {
	var f StructField
	var cur Type = t
	for i, x := range index {
		if i > 0 {
			if cur.Kind() == Pointer && cur.Elem().Kind() == Struct {
				cur = cur.Elem()
			}
		}
		f = cur.Field(x)
		cur = f.Type
	}
	if len(index) > 1 {
		f.Index = append([]int(nil), index...)
	}
	return f
}

func (t *rtype) FieldByName(name string) (StructField, bool) {
	return t.FieldByNameFunc(func(s string) bool { return s == name })
}

// FieldByNameFunc finds the shallowest field whose name satisfies match,
// following embedded structs breadth first. A name found twice at the
// same depth is ambiguous and not found.
func (t *rtype) FieldByNameFunc(match func(string) bool) (StructField, bool) {
	if t.Kind() != Struct {
		panic("reflect: FieldByNameFunc of non-struct type " + t.String())
	}
	type item struct {
		typ   *rtype
		index []int
	}
	current := []item{{t, nil}}
	visited := map[*rtype]bool{}
	for len(current) > 0 {
		var next []item
		var result StructField
		count := 0
		for _, it := range current {
			if visited[it.typ] {
				continue
			}
			visited[it.typ] = true
			st := it.typ.structType()
			for i := 0; i < int(st.NFields); i++ {
				f := structField(st, i)
				index := append(append([]int(nil), it.index...), i)
				if match(f.Name) {
					count++
					if count == 1 {
						f.Index = index
						result = f
					}
					continue
				}
				if f.Anonymous {
					ft := f.Type
					if ft.Kind() == Pointer {
						ft = ft.Elem()
					}
					if ft.Kind() == Struct {
						next = append(next, item{ft.(*rtype), index})
					}
				}
			}
		}
		if count == 1 {
			return result, true
		}
		if count > 1 {
			return StructField{}, false
		}
		current = next
	}
	return StructField{}, false
}

func (t *rtype) Implements(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.Implements")
	}
	if u.Kind() != Interface {
		panic("reflect: non-interface type passed to Type.Implements")
	}
	return implements(u.common(), t.common())
}

func (t *rtype) AssignableTo(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.AssignableTo")
	}
	uu := u.common()
	return directlyAssignable(uu, t.common()) || implements(uu, t.common())
}

func (t *rtype) ConvertibleTo(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.ConvertibleTo")
	}
	return convertOp(u.common(), t.common()) != nil
}

func (t *rtype) CanSeq() bool {
	switch t.Kind() {
	case Int8, Int16, Int32, Int64, Int, Uint8, Uint16, Uint32, Uint64, Uint, Uintptr, Array, Slice, Chan, String, Map:
		return true
	}
	return false
}

func (t *rtype) CanSeq2() bool {
	switch t.Kind() {
	case Array, Slice, String, Map:
		return true
	}
	return false
}

func (t *rtype) OverflowComplex(x complex128) bool {
	k := t.Kind()
	switch k {
	case Complex64:
		return overflowFloat32(real(x)) || overflowFloat32(imag(x))
	case Complex128:
		return false
	}
	panic("reflect: OverflowComplex of non-complex type " + t.String())
}

func (t *rtype) OverflowFloat(x float64) bool {
	k := t.Kind()
	switch k {
	case Float32:
		return overflowFloat32(x)
	case Float64:
		return false
	}
	panic("reflect: OverflowFloat of non-float type " + t.String())
}

func (t *rtype) OverflowInt(x int64) bool {
	k := t.Kind()
	switch k {
	case Int, Int8, Int16, Int32, Int64:
		bitSize := t.Size() * 8
		trunc := (x << (64 - bitSize)) >> (64 - bitSize)
		return x != trunc
	}
	panic("reflect: OverflowInt of non-int type " + t.String())
}

func (t *rtype) OverflowUint(x uint64) bool {
	k := t.Kind()
	switch k {
	case Uint, Uintptr, Uint8, Uint16, Uint32, Uint64:
		bitSize := t.Size() * 8
		trunc := (x << (64 - bitSize)) >> (64 - bitSize)
		return x != trunc
	}
	panic("reflect: OverflowUint of non-uint type " + t.String())
}

func overflowFloat32(x float64) bool {
	if x < 0 {
		x = -x
	}
	return 3.40282346638528859811704183484516925440e+38 < x && x <= 1.7976931348623157e308
}

// implements reports whether the type V implements the interface type T.
func implements(T, V *rtabi.Type) bool {
	if T.Kind() != rtabi.Interface {
		return false
	}
	return rtabi.Implements((*rtabi.InterfaceType)(unsafe.Pointer(T)), V)
}

// directlyAssignable reports whether a value x of type V can be directly
// assigned (using memmove) to a value of type T.
func directlyAssignable(T, V *rtabi.Type) bool {
	if T == V {
		return true
	}
	// Otherwise at least one of T and V must not be defined and they must
	// have the same kind.
	if T.HasName() && V.HasName() || T.Kind() != V.Kind() {
		return false
	}
	if T.Kind() == rtabi.Chan && specialChannelAssignability(T, V) {
		return true
	}
	return haveIdenticalUnderlyingType(T, V, true)
}

func specialChannelAssignability(T, V *rtabi.Type) bool {
	tc := (*rtabi.ChanType)(unsafe.Pointer(T))
	vc := (*rtabi.ChanType)(unsafe.Pointer(V))
	return vc.Dir == uintptr(BothDir) && (T.Name() == "" || V.Name() == "") && haveIdenticalType(tc.Elem, vc.Elem, true)
}

func haveIdenticalType(T, V *rtabi.Type, cmpTags bool) bool {
	if T == V {
		return true
	}
	if T.HasName() || V.HasName() || T.Kind() != V.Kind() {
		// Distinct descriptors of the same defined type are the same type
		// when their names and packages agree (descriptors synthesized by
		// reflect, such as PointerTo results).
		return T.HasName() && V.HasName() && T.Str == V.Str && T.PkgPath() == V.PkgPath() && T.Kind() == V.Kind()
	}
	return haveIdenticalUnderlyingType(T, V, cmpTags)
}

func haveIdenticalUnderlyingType(T, V *rtabi.Type, cmpTags bool) bool {
	if T == V {
		return true
	}
	kind := T.Kind()
	if kind != V.Kind() {
		return false
	}
	if rtabi.Bool <= kind && kind <= rtabi.Complex128 || kind == rtabi.String || kind == rtabi.UnsafePointer {
		return true
	}
	switch kind {
	case rtabi.Array:
		return T.Len() == V.Len() && haveIdenticalType(T.Elem(), V.Elem(), cmpTags)
	case rtabi.Chan:
		return (*rtabi.ChanType)(unsafe.Pointer(V)).Dir == (*rtabi.ChanType)(unsafe.Pointer(T)).Dir && haveIdenticalType(T.Elem(), V.Elem(), cmpTags)
	case rtabi.Func:
		t := (*rtabi.FuncType)(unsafe.Pointer(T))
		v := (*rtabi.FuncType)(unsafe.Pointer(V))
		if t.OutCount != v.OutCount || t.InCount != v.InCount || t.Variadic != v.Variadic {
			return false
		}
		for i := 0; i < int(t.InCount+t.OutCount); i++ {
			if !haveIdenticalType(t.Param(i), v.Param(i), cmpTags) {
				return false
			}
		}
		return true
	case rtabi.Interface:
		t := (*rtabi.InterfaceType)(unsafe.Pointer(T))
		v := (*rtabi.InterfaceType)(unsafe.Pointer(V))
		if t.Mcount == 0 && v.Mcount == 0 {
			return true
		}
		return false
	case rtabi.Map:
		return haveIdenticalType(T.Key(), V.Key(), cmpTags) && haveIdenticalType(T.Elem(), V.Elem(), cmpTags)
	case rtabi.Pointer, rtabi.Slice:
		return haveIdenticalType(T.Elem(), V.Elem(), cmpTags)
	case rtabi.Struct:
		t := (*rtabi.StructType)(unsafe.Pointer(T))
		v := (*rtabi.StructType)(unsafe.Pointer(V))
		if t.NFields != v.NFields {
			return false
		}
		if t.PkgPath != v.PkgPath {
			return false
		}
		for i := 0; i < int(t.NFields); i++ {
			tf := t.Field(i)
			vf := v.Field(i)
			if tf.Name != vf.Name || !haveIdenticalType(tf.Typ, vf.Typ, cmpTags) {
				return false
			}
			if cmpTags && tf.Tag != vf.Tag {
				return false
			}
			if tf.OffsetEmbed != vf.OffsetEmbed {
				return false
			}
		}
		return true
	}
	return false
}

// TypeOf returns the reflection Type that represents the dynamic type of i.
func TypeOf(i any) Type {
	return toType(rtabi.TypeOf(i))
}

// TypeFor returns the Type that represents the type argument T.
func TypeFor[T any]() Type {
	var v T
	if t := TypeOf(v); t != nil {
		return t
	}
	return TypeOf((*T)(nil)).Elem()
}

// ptrMap caches pointer types that reflect had to synthesize.
var ptrMap sync.Map // map[*rtabi.Type]*rtabi.PtrType

// pointerGCData is the pointer bitmap of a pointer: one pointer word.
var pointerGCData byte = 1

// PtrTo returns the pointer type with element t.
func PtrTo(t Type) Type { return PointerTo(t) }

// PointerTo returns the pointer type with element t.
func PointerTo(t Type) Type {
	return toRType(ptrTo(t.common()))
}

func ptrTo(t *rtabi.Type) *rtabi.Type {
	if u := t.Uncommon_; u != nil && u.PtrToThis != nil {
		return u.PtrToThis
	}
	if pi, ok := ptrMap.Load(t); ok {
		return &pi.(*rtabi.PtrType).Type
	}
	pp := new(rtabi.PtrType)
	pp.Size_ = unsafe.Sizeof(uintptr(0))
	pp.PtrBytes = pp.Size_
	pp.GCData = &pointerGCData
	pp.Str = "*" + t.Str
	pp.Kind_ = rtabi.Pointer
	pp.Align_ = uint8(pp.Size_)
	pp.FieldAlign = pp.Align_
	pp.TFlag = rtabi.TFlagComparable | rtabi.TFlagRegularMemory | rtabi.TFlagDirectIface
	pp.Elem = t
	pi, _ := ptrMap.LoadOrStore(t, pp)
	return &pi.(*rtabi.PtrType).Type
}

// SliceOf, MapOf, ArrayOf, ChanOf, FuncOf and StructOf would create new
// types at run time, which marmot does not support.

func SliceOf(t Type) Type       { panic("reflect.SliceOf: not supported by marmot") }
func MapOf(key, elem Type) Type { panic("reflect.MapOf: not supported by marmot") }
func ArrayOf(length int, elem Type) Type {
	panic("reflect.ArrayOf: not supported by marmot")
}
func ChanOf(dir ChanDir, t Type) Type { panic("reflect.ChanOf: not supported by marmot") }
func FuncOf(in, out []Type, variadic bool) Type {
	panic("reflect.FuncOf: not supported by marmot")
}
func StructOf(fields []StructField) Type { panic("reflect.StructOf: not supported by marmot") }

// A StructTag is the tag string in a struct field.
type StructTag string

// Get returns the value associated with key in the tag string.
func (tag StructTag) Get(key string) string {
	v, _ := tag.Lookup(key)
	return v
}

// Lookup returns the value associated with key in the tag string.
func (tag StructTag) Lookup(key string) (value string, ok bool) {
	for tag != "" {
		// Skip leading space.
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}

		// Scan to colon. A space, a quote or a control character is a
		// syntax error.
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
			i++
		}
		if i == 0 || i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			break
		}
		name := string(tag[:i])
		tag = tag[i+1:]

		// Scan quoted string to find value.
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}
		qvalue := string(tag[:i+1])
		tag = tag[i+1:]

		if key == name {
			value, err := strconv.Unquote(qvalue)
			if err != nil {
				break
			}
			return value, true
		}
	}
	return "", false
}
