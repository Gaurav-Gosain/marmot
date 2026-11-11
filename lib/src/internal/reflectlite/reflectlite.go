// Package reflectlite implements a lightweight version of reflect, not
// using any package except for "runtime", "unsafe", and "internal/abi".
//
// marmot overlay: the same API on marmot's type descriptors
// (internal/rtabi). It covers what errors, sort and context use.
package reflectlite

import (
	"internal/rtabi"
	"unsafe"
)

// A Kind represents the specific kind of type that a Type represents.
type Kind = rtabi.Kind

const Ptr = rtabi.Pointer

const (
	Interface = rtabi.Interface
	Slice     = rtabi.Slice
	String    = rtabi.String
	Struct    = rtabi.Struct
)

// Type is the representation of a Go type.
type Type interface {
	Name() string
	PkgPath() string
	Size() uintptr
	Kind() Kind
	Implements(u Type) bool
	AssignableTo(u Type) bool
	Comparable() bool
	String() string
	Elem() Type
	common() *rtabi.Type
	uncommon() *rtabi.UncommonType
}

// rtype is the common implementation of Type.
type rtype struct {
	*rtabi.Type
}

func toRType(t *rtabi.Type) rtype { return rtype{t} }

func (t rtype) common() *rtabi.Type           { return t.Type }
func (t rtype) uncommon() *rtabi.UncommonType { return t.Type.Uncommon() }
func (t rtype) Name() string                  { return t.Type.Name() }
func (t rtype) PkgPath() string               { return t.Type.PkgPath() }
func (t rtype) Size() uintptr                 { return t.Type.Size() }
func (t rtype) Kind() Kind                    { return t.Type.Kind() }
func (t rtype) String() string                { return t.Type.String() }
func (t rtype) Comparable() bool              { return t.Type.Comparable() }

func (t rtype) Elem() Type {
	e := t.Type.Elem()
	if e == nil {
		panic("reflect: Elem of invalid type " + t.String())
	}
	return toRType(e)
}

func (t rtype) Implements(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.Implements")
	}
	if u.Kind() != Interface {
		panic("reflect: non-interface type passed to Type.Implements")
	}
	return rtabi.Implements((*rtabi.InterfaceType)(unsafe.Pointer(u.common())), t.Type)
}

func (t rtype) AssignableTo(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.AssignableTo")
	}
	uu := u.common()
	return directlyAssignable(uu, t.Type) || implements(uu, t.Type)
}

func implements(T, V *rtabi.Type) bool {
	if T.Kind() != Interface {
		return false
	}
	return rtabi.Implements((*rtabi.InterfaceType)(unsafe.Pointer(T)), V)
}

// directlyAssignable reports whether a value of type V can be assigned to
// a variable of type T without conversion. Descriptors are unique, so
// identical types have equal pointers; an unnamed type is also assignable
// to a defined type with the same underlying structure, which marmot
// approximates by the type string of the unnamed side.
func directlyAssignable(T, V *rtabi.Type) bool {
	if T == V {
		return true
	}
	if T.HasName() && V.HasName() || T.Kind() != V.Kind() {
		return false
	}
	if T.Kind() == rtabi.Chan && V.Elem() == T.Elem() {
		return true
	}
	return false
}

// TypeOf returns the reflection Type that represents the dynamic type of i.
func TypeOf(i any) Type {
	t := rtabi.TypeOf(i)
	if t == nil {
		return nil
	}
	return toRType(t)
}

// Value is the reflection interface to a Go value.
type Value struct {
	typ_ *rtabi.Type
	ptr  unsafe.Pointer
	flag
}

type flag uintptr

const (
	flagKindWidth      = 5
	flagKindMask  flag = 1<<flagKindWidth - 1
	flagStickyRO  flag = 1 << 5
	flagEmbedRO   flag = 1 << 6
	flagIndir     flag = 1 << 7
	flagAddr      flag = 1 << 8
	flagMethod    flag = 1 << 9
	flagRO        flag = flagStickyRO | flagEmbedRO
)

func (f flag) kind() Kind { return Kind(f & flagKindMask) }

func (f flag) ro() flag {
	if f&flagRO != 0 {
		return flagStickyRO
	}
	return 0
}

// A ValueError occurs when a Value method is invoked on a Value that does
// not support it.
type ValueError struct {
	Method string
	Kind   Kind
}

func (e *ValueError) Error() string {
	if e.Kind == 0 {
		return "reflect: call of " + e.Method + " on zero Value"
	}
	return "reflect: call of " + e.Method + " on " + kindName(e.Kind) + " Value"
}

var kindNames = []string{
	rtabi.Invalid:       "invalid",
	rtabi.Bool:          "bool",
	rtabi.Int:           "int",
	rtabi.Int8:          "int8",
	rtabi.Int16:         "int16",
	rtabi.Int32:         "int32",
	rtabi.Int64:         "int64",
	rtabi.Uint:          "uint",
	rtabi.Uint8:         "uint8",
	rtabi.Uint16:        "uint16",
	rtabi.Uint32:        "uint32",
	rtabi.Uint64:        "uint64",
	rtabi.Uintptr:       "uintptr",
	rtabi.Float32:       "float32",
	rtabi.Float64:       "float64",
	rtabi.Complex64:     "complex64",
	rtabi.Complex128:    "complex128",
	rtabi.Array:         "array",
	rtabi.Chan:          "chan",
	rtabi.Func:          "func",
	rtabi.Interface:     "interface",
	rtabi.Map:           "map",
	rtabi.Pointer:       "ptr",
	rtabi.Slice:         "slice",
	rtabi.String:        "string",
	rtabi.Struct:        "struct",
	rtabi.UnsafePointer: "unsafe.Pointer",
}

func kindName(k Kind) string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "kind" + itoa(int(k))
}

func itoa(v int) string {
	var b [24]byte
	i := len(b)
	neg := v < 0
	if neg {
		v = -v
	}
	for {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
		if v == 0 {
			break
		}
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (v Value) pointer() unsafe.Pointer {
	if v.flag&flagIndir != 0 {
		return *(*unsafe.Pointer)(v.ptr)
	}
	return v.ptr
}

// ValueOf returns a new Value initialized to the concrete value stored in
// the interface i.
func ValueOf(i any) Value {
	if i == nil {
		return Value{}
	}
	e := (*rtabi.EmptyInterface)(unsafe.Pointer(&i))
	t := e.Type
	f := flag(t.Kind())
	if t.IfaceIndir() {
		f |= flagIndir
	}
	return Value{t, e.Data, f}
}

// CanSet reports whether the value of v can be changed.
func (v Value) CanSet() bool {
	return v.flag&(flagAddr|flagRO) == flagAddr
}

// Elem returns the value that the interface v contains or that the pointer
// v points to.
func (v Value) Elem() Value {
	k := v.kind()
	switch k {
	case Interface:
		var eface any
		if v.typ_.NumMethod() == 0 {
			eface = *(*any)(v.ptr)
		} else {
			ni := (*rtabi.NonEmptyInterface)(v.ptr)
			if ni.ITab == nil {
				return Value{}
			}
			e := (*rtabi.EmptyInterface)(unsafe.Pointer(&eface))
			e.Type = ni.ITab.Typ
			e.Data = ni.Data
		}
		x := ValueOf(eface)
		if x.flag != 0 {
			x.flag |= v.flag.ro()
		}
		return x
	case Ptr:
		ptr := v.ptr
		if v.flag&flagIndir != 0 {
			ptr = *(*unsafe.Pointer)(ptr)
		}
		if ptr == nil {
			return Value{}
		}
		typ := v.typ_.Elem()
		fl := v.flag&flagRO | flagIndir | flagAddr
		fl |= flag(typ.Kind())
		return Value{typ, ptr, fl}
	}
	panic(&ValueError{"reflectlite.Value.Elem", v.kind()})
}

// IsNil reports whether its argument v is nil.
func (v Value) IsNil() bool {
	switch k := v.kind(); k {
	case rtabi.Chan, rtabi.Func, rtabi.Map, Ptr, rtabi.UnsafePointer:
		ptr := v.ptr
		if v.flag&flagIndir != 0 {
			ptr = *(*unsafe.Pointer)(ptr)
		}
		return ptr == nil
	case Interface, Slice:
		return *(*unsafe.Pointer)(v.ptr) == nil
	}
	panic(&ValueError{"reflectlite.Value.IsNil", v.kind()})
}

// IsValid reports whether v represents a value.
func (v Value) IsValid() bool {
	return v.flag != 0
}

// Kind returns v's Kind.
func (v Value) Kind() Kind {
	return v.kind()
}

//go:linkname chanlen runtime.chanlen
func chanlen(unsafe.Pointer) int

//go:linkname maplen runtime.maplen
func maplen(unsafe.Pointer) int

// Len returns v's length.
func (v Value) Len() int {
	switch v.kind() {
	case rtabi.Array:
		return v.typ_.Len()
	case rtabi.Chan:
		return chanlen(v.pointer())
	case rtabi.Map:
		return maplen(v.pointer())
	case Slice:
		return (*sliceHeader)(v.ptr).Len
	case String:
		return (*stringHeader)(v.ptr).Len
	}
	panic(&ValueError{"reflect.Value.Len", v.kind()})
}

type sliceHeader struct {
	Data unsafe.Pointer
	Len  int
	Cap  int
}

type stringHeader struct {
	Data unsafe.Pointer
	Len  int
}

// Set assigns x to the value v.
func (v Value) Set(x Value) {
	if v.flag&(flagAddr|flagRO) != flagAddr {
		panic("reflect: assign to unaddressable or read-only value")
	}
	x = x.assignTo("reflectlite.Set", v.typ_, nil)
	if x.flag&flagIndir != 0 {
		typedmemmove(v.typ_, v.ptr, x.ptr)
	} else {
		*(*unsafe.Pointer)(v.ptr) = x.ptr
	}
}

// Type returns v's type.
func (v Value) Type() Type {
	if v.flag == 0 {
		panic(&ValueError{"reflectlite.Value.Type", 0})
	}
	return toRType(v.typ_)
}

//go:linkname typedmemmove runtime.typedmemmove
func typedmemmove(t *rtabi.Type, dst, src unsafe.Pointer)

//go:linkname unsafe_New runtime.newobject
func unsafe_New(t *rtabi.Type) unsafe.Pointer

//go:linkname assertE2I runtime.assertE2I
func assertE2I(inter *rtabi.InterfaceType, t *rtabi.Type) *rtabi.Itab

// assignTo returns a Value of type dst holding v, converting to an
// interface type if needed.
func (v Value) assignTo(context string, dst *rtabi.Type, target unsafe.Pointer) Value {
	switch {
	case directlyAssignable(dst, v.typ_):
		fl := v.flag&(flagAddr|flagIndir) | v.flag.ro()
		fl |= flag(dst.Kind())
		return Value{dst, v.ptr, fl}
	case implements(dst, v.typ_):
		if target == nil {
			target = unsafe_New(dst)
		}
		if v.Kind() == Interface && v.IsNil() {
			return Value{dst, nil, flag(Interface)}
		}
		x := valueInterface(v)
		if dst.NumMethod() == 0 {
			*(*any)(target) = x
		} else {
			e := (*rtabi.EmptyInterface)(unsafe.Pointer(&x))
			ni := (*rtabi.NonEmptyInterface)(target)
			ni.ITab = assertE2I((*rtabi.InterfaceType)(unsafe.Pointer(dst)), e.Type)
			ni.Data = e.Data
		}
		return Value{dst, target, flagIndir | flag(Interface)}
	}
	panic(context + ": value of type " + v.typ_.String() + " is not assignable to type " + dst.String())
}

// valueInterface returns v as an interface{} value.
func valueInterface(v Value) any {
	if v.flag == 0 {
		panic(&ValueError{"reflectlite.Value.Interface", 0})
	}
	if v.kind() == Interface {
		if v.typ_.NumMethod() == 0 {
			return *(*any)(v.ptr)
		}
		var x any
		ni := (*rtabi.NonEmptyInterface)(v.ptr)
		if ni.ITab != nil {
			e := (*rtabi.EmptyInterface)(unsafe.Pointer(&x))
			e.Type = ni.ITab.Typ
			e.Data = ni.Data
		}
		return x
	}
	return packEface(v)
}

// packEface converts v to the empty interface.
func packEface(v Value) any {
	t := v.typ_
	var i any
	e := (*rtabi.EmptyInterface)(unsafe.Pointer(&i))
	switch {
	case t.IfaceIndir():
		if v.flag&flagIndir == 0 {
			panic("bad indir")
		}
		ptr := v.ptr
		if v.flag&flagAddr != 0 {
			c := unsafe_New(t)
			typedmemmove(t, c, ptr)
			ptr = c
		}
		e.Data = ptr
	case v.flag&flagIndir != 0:
		e.Data = *(*unsafe.Pointer)(v.ptr)
	default:
		e.Data = v.ptr
	}
	e.Type = t
	return i
}

// Swapper returns a function that swaps the elements in the provided
// slice. Swapper panics if the provided interface is not a slice.
func Swapper(slice any) func(i, j int) {
	v := ValueOf(slice)
	if v.Kind() != Slice {
		panic(&ValueError{Method: "Swapper", Kind: v.Kind()})
	}
	s := (*sliceHeader)(v.ptr)
	switch s.Len {
	case 0:
		return func(i, j int) { panic("reflect: slice index out of range") }
	case 1:
		return func(i, j int) {
			if i != 0 || j != 0 {
				panic("reflect: slice index out of range")
			}
		}
	}
	typ := v.typ_.Elem()
	size := typ.Size()
	switch size {
	case 8:
		is := *(*[]int64)(v.ptr)
		return func(i, j int) { is[i], is[j] = is[j], is[i] }
	case 4:
		is := *(*[]int32)(v.ptr)
		return func(i, j int) { is[i], is[j] = is[j], is[i] }
	case 2:
		is := *(*[]int16)(v.ptr)
		return func(i, j int) { is[i], is[j] = is[j], is[i] }
	case 1:
		is := *(*[]int8)(v.ptr)
		return func(i, j int) { is[i], is[j] = is[j], is[i] }
	}
	tmp := unsafe_New(typ)
	return func(i, j int) {
		if uint(i) >= uint(s.Len) || uint(j) >= uint(s.Len) {
			panic("reflect: slice index out of range")
		}
		val1 := unsafe.Add(s.Data, uintptr(i)*size)
		val2 := unsafe.Add(s.Data, uintptr(j)*size)
		typedmemmove(typ, tmp, val1)
		typedmemmove(typ, val1, val2)
		typedmemmove(typ, val2, tmp)
	}
}
