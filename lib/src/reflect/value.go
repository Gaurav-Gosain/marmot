package reflect

import (
	"internal/rtabi"
	"math"
	"strconv"
	"unsafe"
)

// Value is the reflection interface to a Go value.
type Value struct {
	typ_ *rtabi.Type
	// ptr points to the value when flagIndir is set, otherwise it is the
	// value itself (pointer-shaped kinds).
	ptr unsafe.Pointer
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
	return "reflect: call of " + e.Method + " on " + e.Kind.String() + " Value"
}

func (f flag) mustBe(expected Kind) {
	if Kind(f&flagKindMask) != expected {
		panic(&ValueError{valueMethodName(), f.kind()})
	}
}

func (f flag) mustBeExported() {
	if f == 0 {
		panic(&ValueError{valueMethodName(), Invalid})
	}
	if f&flagRO != 0 {
		panic("reflect: " + valueMethodName() + " using value obtained using unexported field")
	}
}

func (f flag) mustBeAssignable() {
	if f == 0 {
		panic(&ValueError{valueMethodName(), Invalid})
	}
	if f&flagRO != 0 {
		panic("reflect: " + valueMethodName() + " using value obtained using unexported field")
	}
	if f&flagAddr == 0 {
		panic("reflect: " + valueMethodName() + " using unaddressable value")
	}
}

// valueMethodName names the calling Value method in panic messages.
// marmot has no call stack information.
func valueMethodName() string { return "reflect.Value method" }

// Runtime entry points.

//go:linkname unsafe_New runtime.newobject
func unsafe_New(t *rtabi.Type) unsafe.Pointer

//go:linkname unsafe_NewArray runtime.newarray
func unsafe_NewArray(t *rtabi.Type, n int) unsafe.Pointer

//go:linkname typedmemmove runtime.typedmemmove
func typedmemmove(t *rtabi.Type, dst, src unsafe.Pointer)

//go:linkname typedmemclr runtime.typedmemclr
func typedmemclr(t *rtabi.Type, p unsafe.Pointer)

//go:linkname memmove runtime.memmove
func memmove(dst, src unsafe.Pointer, n uintptr)

//go:linkname typehash runtime.typehash
func typehash(t *rtabi.Type, p unsafe.Pointer, seed uintptr) uintptr

//go:linkname typeequal runtime.typeequal
func typeequal(t *rtabi.Type, p, q unsafe.Pointer) bool

//go:linkname assertE2I runtime.assertE2I
func assertE2I(inter *rtabi.InterfaceType, t *rtabi.Type) *rtabi.Itab

//go:linkname makeslice runtime.makeslice
func makeslice(et *rtabi.Type, len, cap int) unsafe.Pointer

//go:linkname growslice runtime.growslice
func growslice(et *rtabi.Type, oldPtr unsafe.Pointer, oldLen, oldCap, newLen int) sliceHeader

//go:linkname makemap runtime.makemap
func makemap(t *rtabi.MapType, hint int) unsafe.Pointer

//go:linkname mapaccess2 runtime.mapaccess2
func mapaccess2(t *rtabi.MapType, m unsafe.Pointer, key unsafe.Pointer) (unsafe.Pointer, bool)

//go:linkname mapassign runtime.mapassign
func mapassign(t *rtabi.MapType, m unsafe.Pointer, key unsafe.Pointer) unsafe.Pointer

//go:linkname mapdelete runtime.mapdelete
func mapdelete(t *rtabi.MapType, m unsafe.Pointer, key unsafe.Pointer)

//go:linkname maplen runtime.maplen
func maplen(m unsafe.Pointer) int

//go:linkname mapclear runtime.mapclear
func mapclear(t *rtabi.MapType, m unsafe.Pointer)

//go:linkname mapiterinit runtime.mapiterinit
func mapiterinit(t *rtabi.MapType, m unsafe.Pointer, it *rtabi.Hiter)

//go:linkname mapiternext runtime.mapiternext
func mapiternext(it *rtabi.Hiter)

//go:linkname makechan runtime.makechan
func makechan(elem *rtabi.Type, size int) unsafe.Pointer

//go:linkname chansend runtime.chansend
func chansend(c unsafe.Pointer, elem unsafe.Pointer, block bool) bool

//go:linkname chanrecv runtime.chanrecv
func chanrecv(c unsafe.Pointer, elem unsafe.Pointer, block bool) (selected, received bool)

//go:linkname closechan runtime.closechan
func closechan(c unsafe.Pointer)

//go:linkname chanlen runtime.chanlen
func chanlen(c unsafe.Pointer) int

//go:linkname chancap runtime.chancap
func chancap(c unsafe.Pointer) int

//go:linkname selectgo runtime.selectgo
func selectgo(cases *rtabi.Scase, ncases int, block bool) (int, bool)

type sliceHeader struct {
	Data unsafe.Pointer
	Len  int
	Cap  int
}

type stringHeader struct {
	Data unsafe.Pointer
	Len  int
}

// Descriptors of basic types used to build Values.
var (
	uint8Type  = rtabi.TypeOf(uint8(0))
	stringType = rtabi.TypeOf("")
)

// pointer returns the underlying pointer of a pointer-shaped value.
func (v Value) pointer() unsafe.Pointer {
	if v.flag&flagIndir != 0 {
		return *(*unsafe.Pointer)(v.ptr)
	}
	return v.ptr
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

// unpackEface converts the empty interface i to a Value.
func unpackEface(i any) Value {
	e := (*rtabi.EmptyInterface)(unsafe.Pointer(&i))
	t := e.Type
	if t == nil {
		return Value{}
	}
	f := flag(t.Kind())
	if t.IfaceIndir() {
		f |= flagIndir
	}
	return Value{t, e.Data, f}
}

// ValueOf returns a new Value initialized to the concrete value stored in
// the interface i. ValueOf(nil) returns the zero Value.
func ValueOf(i any) Value {
	if i == nil {
		return Value{}
	}
	return unpackEface(i)
}

// Zero returns a Value representing the zero value for the specified type.
func Zero(typ Type) Value {
	if typ == nil {
		panic("reflect: Zero(nil)")
	}
	t := typ.common()
	fl := flag(t.Kind())
	if t.IfaceIndir() {
		return Value{t, unsafe_New(t), fl | flagIndir}
	}
	return Value{t, nil, fl}
}

// New returns a Value representing a pointer to a new zero value for the
// specified type.
func New(typ Type) Value {
	if typ == nil {
		panic("reflect: New(nil)")
	}
	t := typ.common()
	pt := ptrTo(t)
	ptr := unsafe_New(t)
	return Value{pt, ptr, flag(Pointer)}
}

// NewAt returns a Value representing a pointer to a value of the specified
// type at p.
func NewAt(typ Type, p unsafe.Pointer) Value {
	return Value{ptrTo(typ.common()), p, flag(Pointer)}
}

// Indirect returns the value that v points to.
func Indirect(v Value) Value {
	if v.Kind() != Pointer {
		return v
	}
	return v.Elem()
}

// IsValid reports whether v represents a value.
func (v Value) IsValid() bool { return v.flag != 0 }

// Kind returns v's Kind.
func (v Value) Kind() Kind { return v.kind() }

// Type returns v's type.
func (v Value) Type() Type {
	if v.flag == 0 {
		panic(&ValueError{"reflect.Value.Type", Invalid})
	}
	return toRType(v.typ_)
}

// CanAddr reports whether the value's address can be obtained with Addr.
func (v Value) CanAddr() bool { return v.flag&flagAddr != 0 }

// CanSet reports whether the value of v can be changed.
func (v Value) CanSet() bool { return v.flag&(flagAddr|flagRO) == flagAddr }

// CanInterface reports whether Interface can be used without panicking.
func (v Value) CanInterface() bool {
	if v.flag == 0 {
		panic(&ValueError{"reflect.Value.CanInterface", Invalid})
	}
	return v.flag&flagRO == 0
}

// Interface returns v's current value as an interface{}.
func (v Value) Interface() any {
	return valueInterface(v, true)
}

func valueInterface(v Value, safe bool) any {
	if v.flag == 0 {
		panic(&ValueError{"reflect.Value.Interface", Invalid})
	}
	if safe && v.flag&flagRO != 0 {
		panic("reflect.Value.Interface: cannot return value obtained from unexported field or method")
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

// Addr returns a pointer value representing the address of v.
func (v Value) Addr() Value {
	if v.flag&flagAddr == 0 {
		panic("reflect.Value.Addr of unaddressable value")
	}
	fl := v.flag & flagRO
	return Value{ptrTo(v.typ_), v.ptr, fl | flag(Pointer)}
}

// Bool returns v's underlying value.
func (v Value) Bool() bool {
	if v.kind() != Bool {
		panic(&ValueError{"reflect.Value.Bool", v.kind()})
	}
	return *(*bool)(v.ptr)
}

// Bytes returns v's underlying value (a byte slice or addressable byte
// array).
func (v Value) Bytes() []byte {
	switch v.kind() {
	case Slice:
		if v.typ_.Elem().Kind() != rtabi.Uint8 {
			panic("reflect.Value.Bytes of non-byte slice")
		}
		return *(*[]byte)(v.ptr)
	case Array:
		if v.typ_.Elem().Kind() != rtabi.Uint8 {
			panic("reflect.Value.Bytes of non-byte array")
		}
		if !v.CanAddr() {
			panic("reflect.Value.Bytes of unaddressable byte array")
		}
		n := v.typ_.Len()
		return unsafe.Slice((*byte)(v.ptr), n)
	}
	panic(&ValueError{"reflect.Value.Bytes", v.kind()})
}

func (v Value) runes() []rune {
	v.mustBe(Slice)
	if v.typ_.Elem().Kind() != rtabi.Int32 {
		panic("reflect.Value.Bytes of non-rune slice")
	}
	return *(*[]rune)(v.ptr)
}

// Cap returns v's capacity.
func (v Value) Cap() int {
	switch v.kind() {
	case Array:
		return v.typ_.Len()
	case Chan:
		return chancap(v.pointer())
	case Slice:
		return (*sliceHeader)(v.ptr).Cap
	case Pointer:
		if v.typ_.Elem().Kind() == rtabi.Array {
			return v.typ_.Elem().Len()
		}
	}
	panic(&ValueError{"reflect.Value.Cap", v.kind()})
}

// Len returns v's length.
func (v Value) Len() int {
	switch v.kind() {
	case Array:
		return v.typ_.Len()
	case Chan:
		return chanlen(v.pointer())
	case Map:
		return maplen(v.pointer())
	case Slice:
		return (*sliceHeader)(v.ptr).Len
	case String:
		return (*stringHeader)(v.ptr).Len
	case Pointer:
		if v.typ_.Elem().Kind() == rtabi.Array {
			return v.typ_.Elem().Len()
		}
	}
	panic(&ValueError{"reflect.Value.Len", v.kind()})
}

// Close closes the channel v.
func (v Value) Close() {
	v.mustBe(Chan)
	v.mustBeExported()
	closechan(v.pointer())
}

// Complex returns v's underlying value, as a complex128.
func (v Value) Complex() complex128 {
	switch v.kind() {
	case Complex64:
		return complex128(*(*complex64)(v.ptr))
	case Complex128:
		return *(*complex128)(v.ptr)
	}
	panic(&ValueError{"reflect.Value.Complex", v.kind()})
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
			if ni.ITab != nil {
				e := (*rtabi.EmptyInterface)(unsafe.Pointer(&eface))
				e.Type = ni.ITab.Typ
				e.Data = ni.Data
			}
		}
		x := unpackEface(eface)
		if x.flag != 0 {
			x.flag |= v.flag.ro()
		}
		return x
	case Pointer:
		ptr := v.pointer()
		if ptr == nil {
			return Value{}
		}
		typ := v.typ_.Elem()
		fl := v.flag&flagRO | flagIndir | flagAddr
		fl |= flag(typ.Kind())
		return Value{typ, ptr, fl}
	}
	panic(&ValueError{"reflect.Value.Elem", v.kind()})
}

// Field returns the i'th field of the struct v.
func (v Value) Field(i int) Value {
	if v.kind() != Struct {
		panic(&ValueError{"reflect.Value.Field", v.kind()})
	}
	st := (*rtabi.StructType)(unsafe.Pointer(v.typ_))
	if uint(i) >= uint(st.NFields) {
		panic("reflect: Field index out of range")
	}
	field := st.Field(i)
	typ := field.Typ

	fl := v.flag&(flagStickyRO|flagIndir|flagAddr) | flag(typ.Kind())
	if !isExportedName(field.Name) {
		if field.Embedded() {
			fl |= flagEmbedRO
		} else {
			fl |= flagStickyRO
		}
	}
	if v.flag&flagIndir == 0 {
		// A struct stored directly in an interface word has a single
		// pointer-shaped field, which is the word itself.
		return Value{typ, v.ptr, fl}
	}
	ptr := unsafe.Add(v.ptr, field.Offset())
	return Value{typ, ptr, fl}
}

// FieldByIndex returns the nested field corresponding to index.
func (v Value) FieldByIndex(index []int) Value {
	if len(index) == 1 {
		return v.Field(index[0])
	}
	v.mustBe(Struct)
	for i, x := range index {
		if i > 0 {
			if v.Kind() == Pointer && v.typ_.Elem().Kind() == rtabi.Struct {
				if v.IsNil() {
					panic("reflect: indirection through nil pointer to embedded struct")
				}
				v = v.Elem()
			}
		}
		v = v.Field(x)
	}
	return v
}

// FieldByIndexErr is FieldByIndex that returns an error instead of
// panicking on a nil embedded pointer.
func (v Value) FieldByIndexErr(index []int) (Value, error) {
	if len(index) == 1 {
		return v.Field(index[0]), nil
	}
	v.mustBe(Struct)
	for i, x := range index {
		if i > 0 {
			if v.Kind() == Pointer && v.typ_.Elem().Kind() == rtabi.Struct {
				if v.IsNil() {
					return Value{}, &embeddedNilError{v.typ_.Elem().Name()}
				}
				v = v.Elem()
			}
		}
		v = v.Field(x)
	}
	return v, nil
}

type embeddedNilError struct{ name string }

func (e *embeddedNilError) Error() string {
	return "reflect: indirection through nil pointer to embedded struct field " + e.name
}

// FieldByName returns the struct field with the given name.
func (v Value) FieldByName(name string) Value {
	if f, ok := toRType(v.typ_).FieldByName(name); ok {
		return v.FieldByIndex(f.Index)
	}
	return Value{}
}

// FieldByNameFunc returns the struct field with a name that satisfies the
// match function.
func (v Value) FieldByNameFunc(match func(string) bool) Value {
	if f, ok := toRType(v.typ_).FieldByNameFunc(match); ok {
		return v.FieldByIndex(f.Index)
	}
	return Value{}
}

// Float returns v's underlying value, as a float64.
func (v Value) Float() float64 {
	switch v.kind() {
	case Float32:
		return float64(*(*float32)(v.ptr))
	case Float64:
		return *(*float64)(v.ptr)
	}
	panic(&ValueError{"reflect.Value.Float", v.kind()})
}

// Index returns v's i'th element.
func (v Value) Index(i int) Value {
	switch v.kind() {
	case Array:
		typ := v.typ_.Elem()
		if uint(i) >= uint(v.typ_.Len()) {
			panic("reflect: array index out of range")
		}
		fl := v.flag&(flagIndir|flagAddr) | v.flag.ro() | flag(typ.Kind())
		if v.flag&flagIndir == 0 {
			return Value{typ, v.ptr, fl}
		}
		return Value{typ, unsafe.Add(v.ptr, uintptr(i)*typ.Size()), fl}
	case Slice:
		s := (*sliceHeader)(v.ptr)
		if uint(i) >= uint(s.Len) {
			panic("reflect: slice index out of range")
		}
		typ := v.typ_.Elem()
		val := unsafe.Add(s.Data, uintptr(i)*typ.Size())
		fl := flagAddr | flagIndir | v.flag.ro() | flag(typ.Kind())
		return Value{typ, val, fl}
	case String:
		s := (*stringHeader)(v.ptr)
		if uint(i) >= uint(s.Len) {
			panic("reflect: string index out of range")
		}
		p := unsafe.Add(s.Data, i)
		fl := v.flag.ro() | flag(Uint8) | flagIndir
		return Value{uint8Type, p, fl}
	}
	panic(&ValueError{"reflect.Value.Index", v.kind()})
}

// Int returns v's underlying value, as an int64.
func (v Value) Int() int64 {
	p := v.ptr
	switch v.kind() {
	case Int:
		return int64(*(*int)(p))
	case Int8:
		return int64(*(*int8)(p))
	case Int16:
		return int64(*(*int16)(p))
	case Int32:
		return int64(*(*int32)(p))
	case Int64:
		return *(*int64)(p)
	}
	panic(&ValueError{"reflect.Value.Int", v.kind()})
}

// Uint returns v's underlying value, as a uint64.
func (v Value) Uint() uint64 {
	p := v.ptr
	switch v.kind() {
	case Uint:
		return uint64(*(*uint)(p))
	case Uint8:
		return uint64(*(*uint8)(p))
	case Uint16:
		return uint64(*(*uint16)(p))
	case Uint32:
		return uint64(*(*uint32)(p))
	case Uint64:
		return *(*uint64)(p)
	case Uintptr:
		return uint64(*(*uintptr)(p))
	}
	panic(&ValueError{"reflect.Value.Uint", v.kind()})
}

// CanInt, CanUint, CanFloat and CanComplex report whether the matching
// accessor can be used without panicking.
func (v Value) CanInt() bool {
	switch v.kind() {
	case Int, Int8, Int16, Int32, Int64:
		return true
	}
	return false
}

func (v Value) CanUint() bool {
	switch v.kind() {
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		return true
	}
	return false
}

func (v Value) CanFloat() bool {
	switch v.kind() {
	case Float32, Float64:
		return true
	}
	return false
}

func (v Value) CanComplex() bool {
	switch v.kind() {
	case Complex64, Complex128:
		return true
	}
	return false
}

// IsNil reports whether its argument v is nil.
func (v Value) IsNil() bool {
	switch v.kind() {
	case Chan, Func, Map, Pointer, UnsafePointer:
		return v.pointer() == nil
	case Interface, Slice:
		return *(*unsafe.Pointer)(v.ptr) == nil
	}
	panic(&ValueError{"reflect.Value.IsNil", v.kind()})
}

// IsZero reports whether v is the zero value for its type.
func (v Value) IsZero() bool {
	switch v.kind() {
	case Bool:
		return !v.Bool()
	case Int, Int8, Int16, Int32, Int64:
		return v.Int() == 0
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		return v.Uint() == 0
	case Float32, Float64:
		return v.Float() == 0 && !math.Signbit(v.Float())
	case Complex64, Complex128:
		c := v.Complex()
		return real(c) == 0 && imag(c) == 0 && !math.Signbit(real(c)) && !math.Signbit(imag(c))
	case Array:
		n := v.Len()
		for i := 0; i < n; i++ {
			if !v.Index(i).IsZero() {
				return false
			}
		}
		return true
	case Chan, Func, Interface, Map, Pointer, Slice, UnsafePointer:
		return v.IsNil()
	case String:
		return v.Len() == 0
	case Struct:
		n := v.NumField()
		for i := 0; i < n; i++ {
			if !v.Field(i).IsZero() {
				return false
			}
		}
		return true
	}
	panic(&ValueError{"reflect.Value.IsZero", v.Kind()})
}

// SetZero sets v to be the zero value of v's type.
func (v Value) SetZero() {
	v.mustBeAssignable()
	typedmemclr(v.typ_, v.ptr)
}

// NumField returns the number of fields in the struct v.
func (v Value) NumField() int {
	v.mustBe(Struct)
	return int((*rtabi.StructType)(unsafe.Pointer(v.typ_)).NFields)
}

// NumMethod returns the number of methods in the value's method set.
func (v Value) NumMethod() int {
	if v.typ_ == nil {
		panic(&ValueError{"reflect.Value.NumMethod", Invalid})
	}
	return v.typ_.NumMethod()
}

// Method returns a function value corresponding to v's i'th method. The
// arguments to a Call on the returned function should not include a
// receiver.
func (v Value) Method(i int) Value {
	if v.typ_ == nil {
		panic(&ValueError{"reflect.Value.Method", Invalid})
	}
	if uint(i) >= uint(v.typ_.NumMethod()) {
		panic("reflect: Method index out of range")
	}
	if v.typ_.Kind() == rtabi.Interface && v.IsNil() {
		panic("reflect: Method on nil interface value")
	}
	return v.methodValue(toRType(v.typ_).Method(i).Name)
}

// MethodByName returns a function value corresponding to the method of v
// with the given name, or the zero Value if there is none.
func (v Value) MethodByName(name string) Value {
	if v.typ_ == nil {
		panic(&ValueError{"reflect.Value.MethodByName", Invalid})
	}
	if v.typ_.Kind() == rtabi.Interface && v.IsNil() {
		panic("reflect: MethodByName on nil interface value")
	}
	if _, ok := toRType(v.typ_).MethodByName(name); !ok {
		return Value{}
	}
	return v.methodValue(name)
}

//go:linkname mallocConservative runtime.mallocConservative
func mallocConservative(size uintptr) unsafe.Pointer

// methodValue builds a func Value for the exported method name of v's
// dynamic type: a closure object {tfn, recv} where recv is the interface
// data word of the receiver.
func (v Value) methodValue(name string) Value {
	if v.flag&flagRO != 0 {
		panic("reflect: Method on value obtained from unexported field")
	}
	recv := v
	if v.kind() == Interface {
		recv = v.Elem()
	}
	x := packEface(recv)
	e := (*rtabi.EmptyInterface)(unsafe.Pointer(&x))
	u := e.Type.Uncommon()
	if u != nil {
		for j := 0; j < int(u.Mcount); j++ {
			m := u.MethodAt(j)
			if m.Name != name {
				continue
			}
			if m.Tfn == nil {
				panic("reflect: method " + name + " of " + e.Type.String() + " is not callable through reflect (no wrapper emitted)")
			}
			obj := (*[2]unsafe.Pointer)(mallocConservative(2 * unsafe.Sizeof(uintptr(0))))
			obj[0] = m.Tfn
			obj[1] = e.Data
			return Value{m.Mtyp, unsafe.Pointer(obj), v.flag.ro() | flag(Func)}
		}
	}
	panic("reflect: method " + name + " not found on " + e.Type.String())
}

// Call calls the function v with the input arguments in.
func (v Value) Call(in []Value) []Value {
	v.mustBe(Func)
	v.mustBeExported()
	return v.call("Call", in)
}

// CallSlice calls the variadic function v with the input arguments in,
// assigning the slice in[len(in)-1] to v's final variadic argument.
func (v Value) CallSlice(in []Value) []Value {
	v.mustBe(Func)
	v.mustBeExported()
	return v.call("CallSlice", in)
}

func alignUp(n, a uintptr) uintptr {
	if a == 0 {
		return n
	}
	return (n + a - 1) &^ (a - 1)
}

// frameLayout computes the offsets of types laid out as a Go struct and
// the total size.
func frameLayout(ts []*rtabi.Type) ([]uintptr, uintptr) {
	offs := make([]uintptr, len(ts))
	var off, maxAlign uintptr = 0, 1
	for i, t := range ts {
		a := uintptr(t.Align_)
		off = alignUp(off, a)
		offs[i] = off
		off += t.Size()
		if a > maxAlign {
			maxAlign = a
		}
	}
	return offs, alignUp(off, maxAlign)
}

type callAdapter func(closure, args, results unsafe.Pointer)

func (v Value) call(op string, in []Value) []Value {
	ft := (*rtabi.FuncType)(unsafe.Pointer(v.typ_))
	fn := v.pointer()
	if fn == nil {
		panic("reflect: call of nil function")
	}
	if ft.Call == nil {
		panic("reflect." + op + ": no call adapter for " + v.typ_.String() + " (the compiler did not emit one)")
	}
	isSlice := op == "CallSlice"
	n := int(ft.InCount)
	isVariadic := ft.Variadic != 0
	if isSlice {
		if !isVariadic {
			panic("reflect: CallSlice of non-variadic function")
		}
		if len(in) < n {
			panic("reflect: CallSlice with too few input arguments")
		}
		if len(in) > n {
			panic("reflect: CallSlice with too many input arguments")
		}
	} else {
		if isVariadic {
			n--
		}
		if len(in) < n {
			panic("reflect: Call with too few input arguments")
		}
		if !isVariadic && len(in) > n {
			panic("reflect: Call with too many input arguments")
		}
	}
	for _, x := range in {
		if x.Kind() == Invalid {
			panic("reflect: " + op + " using zero Value argument")
		}
	}
	if !isSlice && isVariadic {
		// Pack the trailing arguments into a slice.
		m := len(in) - n
		slice := MakeSlice(toRType(ft.Param(n)), m, m)
		elem := toRType(ft.Param(n)).Elem()
		for i := 0; i < m; i++ {
			x := in[n+i]
			if xt := x.Type(); !xt.AssignableTo(elem) {
				panic("reflect: cannot use " + xt.String() + " as type " + elem.String() + " in " + op)
			}
			slice.Index(i).Set(x)
		}
		origIn := in
		in = make([]Value, n+1)
		copy(in[:n], origIn)
		in[n] = slice
	}

	nin := int(ft.InCount)
	nout := int(ft.OutCount)
	ins := make([]*rtabi.Type, nin)
	for i := range ins {
		ins[i] = ft.Param(i)
	}
	outs := make([]*rtabi.Type, nout)
	for i := range outs {
		outs[i] = ft.Param(nin + i)
	}
	inOffs, inSize := frameLayout(ins)
	outOffs, outSize := frameLayout(outs)

	var args, results unsafe.Pointer
	if inSize > 0 {
		args = mallocConservative(inSize)
	}
	if outSize > 0 {
		results = mallocConservative(outSize)
	}
	for i, x := range in {
		targ := ins[i]
		x = x.assignTo("reflect.Value.Call", targ, nil)
		dst := unsafe.Add(args, inOffs[i])
		if x.flag&flagIndir != 0 {
			typedmemmove(targ, dst, x.ptr)
		} else {
			*(*unsafe.Pointer)(dst) = x.ptr
		}
	}

	// Call the adapter's code like a func value: the extra hidden closure
	// parameter is ignored by the C calling convention.
	adapterObj := &struct{ code unsafe.Pointer }{ft.Call}
	adapterPtr := unsafe.Pointer(adapterObj)
	call := *(*callAdapter)(unsafe.Pointer(&adapterPtr))
	call(fn, args, results)

	ret := make([]Value, nout)
	for i, t := range outs {
		ret[i] = copyVal(t, flag(t.Kind()), unsafe.Add(results, outOffs[i]))
	}
	return ret
}

// MakeFunc is not supported by marmot.
func MakeFunc(typ Type, fn func(args []Value) (results []Value)) Value {
	panic("reflect.MakeFunc: not supported by marmot")
}

// Pointer returns v's value as a uintptr.
func (v Value) Pointer() uintptr {
	switch v.kind() {
	case Pointer, Chan, Map, UnsafePointer, Func:
		return uintptr(v.pointer())
	case Slice:
		return uintptr((*sliceHeader)(v.ptr).Data)
	case String:
		return uintptr((*stringHeader)(v.ptr).Data)
	}
	panic(&ValueError{"reflect.Value.Pointer", v.kind()})
}

// UnsafePointer returns v's value as an unsafe.Pointer.
func (v Value) UnsafePointer() unsafe.Pointer {
	return unsafe.Pointer(v.Pointer())
}

// UnsafeAddr returns a pointer to v's data.
func (v Value) UnsafeAddr() uintptr {
	if v.flag&flagAddr == 0 {
		panic("reflect.Value.UnsafeAddr of unaddressable value")
	}
	return uintptr(v.ptr)
}

// Set assigns x to the value v.
func (v Value) Set(x Value) {
	v.mustBeAssignable()
	x.mustBeExported()
	var target unsafe.Pointer
	if v.kind() == Interface {
		target = v.ptr
	}
	x = x.assignTo("reflect.Set", v.typ_, target)
	if x.flag&flagIndir != 0 {
		if x.ptr == unsafe.Pointer(&zeroVal[0]) {
			typedmemclr(v.typ_, v.ptr)
		} else {
			typedmemmove(v.typ_, v.ptr, x.ptr)
		}
	} else {
		*(*unsafe.Pointer)(v.ptr) = x.ptr
	}
}

var zeroVal [1]byte

// assignTo returns a Value of type dst holding v, converting to an
// interface type if needed.
func (v Value) assignTo(context string, dst *rtabi.Type, target unsafe.Pointer) Value {
	switch {
	case directlyAssignable(dst, v.typ_):
		fl := v.flag&(flagAddr|flagIndir) | v.flag.ro()
		fl |= flag(dst.Kind())
		return Value{dst, v.ptr, fl}
	case implements(dst, v.typ_):
		if v.Kind() == Interface && v.IsNil() {
			return Value{dst, nil, flag(Interface)}
		}
		x := valueInterface(v, false)
		if target == nil {
			target = unsafe_New(dst)
		}
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

// SetBool sets v's underlying value.
func (v Value) SetBool(x bool) {
	v.mustBeAssignable()
	v.mustBe(Bool)
	*(*bool)(v.ptr) = x
}

// SetBytes sets v's underlying value.
func (v Value) SetBytes(x []byte) {
	v.mustBeAssignable()
	v.mustBe(Slice)
	if v.typ_.Elem().Kind() != rtabi.Uint8 {
		panic("reflect.Value.SetBytes of non-byte slice")
	}
	*(*[]byte)(v.ptr) = x
}

func (v Value) setRunes(x []rune) {
	v.mustBeAssignable()
	v.mustBe(Slice)
	*(*[]rune)(v.ptr) = x
}

// SetComplex sets v's underlying value to x.
func (v Value) SetComplex(x complex128) {
	v.mustBeAssignable()
	switch v.kind() {
	default:
		panic(&ValueError{"reflect.Value.SetComplex", v.kind()})
	case Complex64:
		*(*complex64)(v.ptr) = complex64(x)
	case Complex128:
		*(*complex128)(v.ptr) = x
	}
}

// SetFloat sets v's underlying value to x.
func (v Value) SetFloat(x float64) {
	v.mustBeAssignable()
	switch v.kind() {
	default:
		panic(&ValueError{"reflect.Value.SetFloat", v.kind()})
	case Float32:
		*(*float32)(v.ptr) = float32(x)
	case Float64:
		*(*float64)(v.ptr) = x
	}
}

// SetInt sets v's underlying value to x.
func (v Value) SetInt(x int64) {
	v.mustBeAssignable()
	switch v.kind() {
	default:
		panic(&ValueError{"reflect.Value.SetInt", v.kind()})
	case Int:
		*(*int)(v.ptr) = int(x)
	case Int8:
		*(*int8)(v.ptr) = int8(x)
	case Int16:
		*(*int16)(v.ptr) = int16(x)
	case Int32:
		*(*int32)(v.ptr) = int32(x)
	case Int64:
		*(*int64)(v.ptr) = x
	}
}

// SetUint sets v's underlying value to x.
func (v Value) SetUint(x uint64) {
	v.mustBeAssignable()
	switch v.kind() {
	default:
		panic(&ValueError{"reflect.Value.SetUint", v.kind()})
	case Uint:
		*(*uint)(v.ptr) = uint(x)
	case Uint8:
		*(*uint8)(v.ptr) = uint8(x)
	case Uint16:
		*(*uint16)(v.ptr) = uint16(x)
	case Uint32:
		*(*uint32)(v.ptr) = uint32(x)
	case Uint64:
		*(*uint64)(v.ptr) = x
	case Uintptr:
		*(*uintptr)(v.ptr) = uintptr(x)
	}
}

// SetString sets v's underlying value to x.
func (v Value) SetString(x string) {
	v.mustBeAssignable()
	v.mustBe(String)
	*(*string)(v.ptr) = x
}

// SetPointer sets the unsafe.Pointer value v to x.
func (v Value) SetPointer(x unsafe.Pointer) {
	v.mustBeAssignable()
	v.mustBe(UnsafePointer)
	*(*unsafe.Pointer)(v.ptr) = x
}

// SetLen sets v's length to n.
func (v Value) SetLen(n int) {
	v.mustBeAssignable()
	v.mustBe(Slice)
	s := (*sliceHeader)(v.ptr)
	if uint(n) > uint(s.Cap) {
		panic("reflect: slice length out of range in SetLen")
	}
	s.Len = n
}

// SetCap sets v's capacity to n.
func (v Value) SetCap(n int) {
	v.mustBeAssignable()
	v.mustBe(Slice)
	s := (*sliceHeader)(v.ptr)
	if n < s.Len || n > s.Cap {
		panic("reflect: slice capacity out of range in SetCap")
	}
	s.Cap = n
}

// Slice returns v[i:j].
func (v Value) Slice(i, j int) Value {
	var (
		cap  int
		typ  *rtabi.Type
		base unsafe.Pointer
	)
	switch kind := v.kind(); kind {
	default:
		panic(&ValueError{"reflect.Value.Slice", v.kind()})
	case Array:
		if v.flag&flagAddr == 0 {
			panic("reflect.Value.Slice: slice of unaddressable array")
		}
		cap = v.typ_.Len()
		typ = sliceOfElem(v.typ_.Elem())
		if typ == nil {
			panic("reflect.Value.Slice: slice of array of " + v.typ_.Elem().String() + " needs a []" + v.typ_.Elem().String() + " descriptor")
		}
		base = v.ptr
	case Slice:
		typ = v.typ_
		s := (*sliceHeader)(v.ptr)
		base = s.Data
		cap = s.Cap
	case String:
		s := (*stringHeader)(v.ptr)
		if i < 0 || j < i || j > s.Len {
			panic("reflect.Value.Slice: string slice index out of bounds")
		}
		t := new(stringHeader)
		if i < s.Len {
			t.Data = unsafe.Add(s.Data, i)
		}
		t.Len = j - i
		return Value{v.typ_, unsafe.Pointer(t), v.flag.ro() | flagIndir | flag(String)}
	}
	if i < 0 || j < i || j > cap {
		panic("reflect.Value.Slice: slice index out of bounds")
	}
	s := new(sliceHeader)
	s.Len = j - i
	s.Cap = cap - i
	if cap-i > 0 {
		s.Data = unsafe.Add(base, uintptr(i)*typ.Elem().Size())
	} else {
		s.Data = base
	}
	return Value{typ, unsafe.Pointer(s), v.flag.ro() | flagIndir | flag(Slice)}
}

// Slice3 is the 3-index form of the slice operation: it returns v[i:j:k].
func (v Value) Slice3(i, j, k int) Value {
	var (
		cap  int
		typ  *rtabi.Type
		base unsafe.Pointer
	)
	switch kind := v.kind(); kind {
	default:
		panic(&ValueError{"reflect.Value.Slice3", v.kind()})
	case Array:
		if v.flag&flagAddr == 0 {
			panic("reflect.Value.Slice3: slice of unaddressable array")
		}
		cap = v.typ_.Len()
		typ = sliceOfElem(v.typ_.Elem())
		if typ == nil {
			panic("reflect.Value.Slice3: slice of array needs a slice descriptor")
		}
		base = v.ptr
	case Slice:
		typ = v.typ_
		s := (*sliceHeader)(v.ptr)
		base = s.Data
		cap = s.Cap
	}
	if i < 0 || j < i || k < j || k > cap {
		panic("reflect.Value.Slice3: slice index out of bounds")
	}
	s := new(sliceHeader)
	s.Len = j - i
	s.Cap = k - i
	if k-i > 0 {
		s.Data = unsafe.Add(base, uintptr(i)*typ.Elem().Size())
	} else {
		s.Data = base
	}
	return Value{typ, unsafe.Pointer(s), v.flag.ro() | flagIndir | flag(Slice)}
}

// sliceOfElem returns the descriptor of []elem when the program has one
// for a byte or rune element (the common case of slicing arrays), else nil.
func sliceOfElem(elem *rtabi.Type) *rtabi.Type {
	switch elem {
	case uint8Type:
		return rtabi.TypeOf([]byte(nil))
	case rtabi.TypeOf(rune(0)):
		return rtabi.TypeOf([]rune(nil))
	case rtabi.TypeOf(0):
		return rtabi.TypeOf([]int(nil))
	case stringType:
		return rtabi.TypeOf([]string(nil))
	}
	return nil
}

// String returns the string v's underlying value, as a string. For other
// kinds it returns "<T Value>".
func (v Value) String() string {
	if v.kind() == String {
		return *(*string)(v.ptr)
	}
	if v.kind() == Invalid {
		return "<invalid Value>"
	}
	return "<" + v.Type().String() + " Value>"
}

// Comparable reports whether the value v is comparable.
func (v Value) Comparable() bool {
	switch v.Kind() {
	case Invalid:
		return false
	case Array:
		n := v.Len()
		for i := 0; i < n; i++ {
			if !v.Index(i).Comparable() {
				return false
			}
		}
		return true
	case Interface:
		return v.IsNil() || v.Elem().Comparable()
	case Struct:
		for i := 0; i < v.NumField(); i++ {
			if !v.Field(i).Comparable() {
				return false
			}
		}
		return true
	}
	return v.typ_.Comparable()
}

// Equal reports true if v is equal to u.
func (v Value) Equal(u Value) bool {
	if v.Kind() == Interface {
		v = v.Elem()
	}
	if u.Kind() == Interface {
		u = u.Elem()
	}
	if !v.IsValid() || !u.IsValid() {
		return v.IsValid() == u.IsValid()
	}
	if v.Kind() != u.Kind() || v.typ_ != u.typ_ {
		return false
	}
	if !v.typ_.Comparable() {
		panic("reflect.Value.Equal: values of type " + v.typ_.String() + " are not comparable")
	}
	vp, up := v.ptr, u.ptr
	if v.flag&flagIndir == 0 {
		vp = unsafe.Pointer(&v.ptr)
	}
	if u.flag&flagIndir == 0 {
		up = unsafe.Pointer(&u.ptr)
	}
	return typeequal(v.typ_, vp, up)
}

// Maps.

// MapIndex returns the value associated with key in the map v.
func (v Value) MapIndex(key Value) Value {
	v.mustBe(Map)
	tt := (*rtabi.MapType)(unsafe.Pointer(v.typ_))
	key = key.assignTo("reflect.Value.MapIndex", tt.Key, nil)
	var k unsafe.Pointer
	if key.flag&flagIndir != 0 {
		k = key.ptr
	} else {
		k = unsafe.Pointer(&key.ptr)
	}
	if v.pointer() == nil {
		return Value{}
	}
	e, ok := mapaccess2(tt, v.pointer(), k)
	if !ok {
		return Value{}
	}
	typ := tt.Elem
	fl := (v.flag | key.flag).ro() | flag(typ.Kind())
	return copyVal(typ, fl, e)
}

// copyVal returns a Value holding a copy of the value of type typ at ptr.
func copyVal(typ *rtabi.Type, fl flag, ptr unsafe.Pointer) Value {
	if typ.IfaceIndir() {
		c := unsafe_New(typ)
		typedmemmove(typ, c, ptr)
		return Value{typ, c, fl | flagIndir}
	}
	return Value{typ, *(*unsafe.Pointer)(ptr), fl}
}

// MapKeys returns a slice containing all the keys present in the map, in
// unspecified order.
func (v Value) MapKeys() []Value {
	v.mustBe(Map)
	tt := (*rtabi.MapType)(unsafe.Pointer(v.typ_))
	keyType := tt.Key
	fl := v.flag.ro() | flag(keyType.Kind())
	m := v.pointer()
	mlen := 0
	if m != nil {
		mlen = maplen(m)
	}
	var it rtabi.Hiter
	mapiterinit(tt, m, &it)
	a := make([]Value, 0, mlen)
	for ; it.Key != nil; mapiternext(&it) {
		a = append(a, copyVal(keyType, fl, it.Key))
	}
	return a
}

// A MapIter is an iterator for ranging over a map.
type MapIter struct {
	m     Value
	hiter rtabi.Hiter
	init  bool
}

// MapRange returns a range iterator for a map.
func (v Value) MapRange() *MapIter {
	if v.kind() != Map {
		panic(&ValueError{"reflect.Value.MapRange", v.kind()})
	}
	return &MapIter{m: v}
}

// Next advances the map iterator and reports whether there is another
// entry.
func (iter *MapIter) Next() bool {
	if !iter.m.IsValid() {
		panic("MapIter.Next called on an iterator that does not have an associated map Value")
	}
	if !iter.init {
		iter.init = true
		mapiterinit((*rtabi.MapType)(unsafe.Pointer(iter.m.typ_)), iter.m.pointer(), &iter.hiter)
	} else {
		if iter.hiter.Key == nil {
			panic("MapIter.Next called on exhausted iterator")
		}
		mapiternext(&iter.hiter)
	}
	return iter.hiter.Key != nil
}

// Key returns the key of iter's current map entry.
func (iter *MapIter) Key() Value {
	if !iter.init || iter.hiter.Key == nil {
		panic("MapIter.Key called before Next or after exhaustion")
	}
	t := (*rtabi.MapType)(unsafe.Pointer(iter.m.typ_))
	return copyVal(t.Key, iter.m.flag.ro()|flag(t.Key.Kind()), iter.hiter.Key)
}

// Value returns the value of iter's current map entry.
func (iter *MapIter) Value() Value {
	if !iter.init || iter.hiter.Key == nil {
		panic("MapIter.Value called before Next or after exhaustion")
	}
	t := (*rtabi.MapType)(unsafe.Pointer(iter.m.typ_))
	return copyVal(t.Elem, iter.m.flag.ro()|flag(t.Elem.Kind()), iter.hiter.Elem)
}

// Reset modifies iter to iterate over v.
func (iter *MapIter) Reset(v Value) {
	if v.IsValid() {
		v.mustBe(Map)
	}
	iter.m = v
	iter.hiter = rtabi.Hiter{}
	iter.init = false
}

// SetIterKey assigns to v the key of iter's current map entry.
func (v Value) SetIterKey(iter *MapIter) {
	v.Set(iter.Key())
}

// SetIterValue assigns to v the value of iter's current map entry.
func (v Value) SetIterValue(iter *MapIter) {
	v.Set(iter.Value())
}

// SetMapIndex sets the element associated with key in the map v to elem.
// If elem is the zero Value, SetMapIndex deletes the key from the map.
func (v Value) SetMapIndex(key, elem Value) {
	v.mustBe(Map)
	v.mustBeExported()
	key.mustBeExported()
	tt := (*rtabi.MapType)(unsafe.Pointer(v.typ_))
	key = key.assignTo("reflect.Value.SetMapIndex", tt.Key, nil)
	var k unsafe.Pointer
	if key.flag&flagIndir != 0 {
		k = key.ptr
	} else {
		k = unsafe.Pointer(&key.ptr)
	}
	if elem.typ_ == nil {
		mapdelete(tt, v.pointer(), k)
		return
	}
	elem.mustBeExported()
	elem = elem.assignTo("reflect.Value.SetMapIndex", tt.Elem, nil)
	var e unsafe.Pointer
	if elem.flag&flagIndir != 0 {
		e = elem.ptr
	} else {
		e = unsafe.Pointer(&elem.ptr)
	}
	slot := mapassign(tt, v.pointer(), k)
	typedmemmove(tt.Elem, slot, e)
}

// Clear clears the contents of a map or zeros the contents of a slice.
func (v Value) Clear() {
	switch v.Kind() {
	case Slice:
		sh := *(*sliceHeader)(v.ptr)
		st := v.typ_.Elem()
		for i := 0; i < sh.Len; i++ {
			typedmemclr(st, unsafe.Add(sh.Data, uintptr(i)*st.Size()))
		}
	case Map:
		mapclear((*rtabi.MapType)(unsafe.Pointer(v.typ_)), v.pointer())
	default:
		panic(&ValueError{"reflect.Value.Clear", v.Kind()})
	}
}

// MakeMap creates a new map with the specified type.
func MakeMap(typ Type) Value {
	return MakeMapWithSize(typ, 0)
}

// MakeMapWithSize creates a new map with the specified type and initial
// space for approximately n elements.
func MakeMapWithSize(typ Type, n int) Value {
	if typ.Kind() != Map {
		panic("reflect.MakeMapWithSize of non-map type")
	}
	t := typ.common()
	m := makemap((*rtabi.MapType)(unsafe.Pointer(t)), n)
	return Value{t, m, flag(Map)}
}

// Slices.

// MakeSlice creates a new zero-initialized slice value for the specified
// slice type, length, and capacity.
func MakeSlice(typ Type, len, cap int) Value {
	if typ.Kind() != Slice {
		panic("reflect.MakeSlice of non-slice type")
	}
	if len < 0 {
		panic("reflect.MakeSlice: negative len")
	}
	if cap < 0 {
		panic("reflect.MakeSlice: negative cap")
	}
	if len > cap {
		panic("reflect.MakeSlice: len > cap")
	}
	s := sliceHeader{makeslice(typ.Elem().common(), len, cap), len, cap}
	return Value{typ.common(), unsafe.Pointer(&s), flagIndir | flag(Slice)}
}

// grow grows the slice s so that it can hold extra more values, allocating
// more capacity if needed. It also returns the old and new slice lengths.
func grow(s Value, extra int) (Value, int, int) {
	i0 := s.Len()
	i1 := i0 + extra
	if i1 < i0 {
		panic("reflect.Append: slice overflow")
	}
	m := s.Cap()
	if i1 <= m {
		return s.Slice(0, i1), i0, i1
	}
	p := (*sliceHeader)(s.ptr)
	g := growslice(s.typ_.Elem(), p.Data, p.Len, p.Cap, i1)
	ns := new(sliceHeader)
	*ns = g
	return Value{s.typ_, unsafe.Pointer(ns), flagIndir | flag(Slice)}, i0, i1
}

// Grow increases the slice's capacity, if necessary, to guarantee space
// for another n elements.
func (v Value) Grow(n int) {
	v.mustBeAssignable()
	v.mustBe(Slice)
	if n < 0 {
		panic("reflect.Value.Grow: negative len")
	}
	p := (*sliceHeader)(v.ptr)
	if p.Len+n > p.Cap {
		g := growslice(v.typ_.Elem(), p.Data, p.Len, p.Cap, p.Len+n)
		g.Len = p.Len
		*p = g
	}
}

// Append appends the values x to a slice s and returns the resulting
// slice.
func Append(s Value, x ...Value) Value {
	s.mustBe(Slice)
	n := s.Len()
	s, i0, i1 := grow(s, len(x))
	for i, j := i0, 0; i < i1; i, j = i+1, j+1 {
		s.Index(i).Set(x[j])
	}
	_ = n
	return s
}

// AppendSlice appends a slice t to a slice s and returns the resulting
// slice.
func AppendSlice(s, t Value) Value {
	s.mustBe(Slice)
	t.mustBe(Slice)
	ns, i0, i1 := grow(s, t.Len())
	Copy(ns.Slice(i0, i1), t)
	return ns
}

// Copy copies the contents of src into dst until either dst has been
// filled or src has been exhausted. It returns the number of elements
// copied.
func Copy(dst, src Value) int {
	dk := dst.kind()
	if dk != Array && dk != Slice {
		panic(&ValueError{"reflect.Copy", dk})
	}
	if dk == Array {
		dst.mustBeAssignable()
	}
	dst.mustBeExported()
	sk := src.kind()
	var stringCopy bool
	if sk != Array && sk != Slice {
		stringCopy = sk == String && dst.typ_.Elem().Kind() == rtabi.Uint8
		if !stringCopy {
			panic(&ValueError{"reflect.Copy", sk})
		}
	}
	src.mustBeExported()
	de := dst.typ_.Elem()
	n := dst.Len()
	if m := src.Len(); m < n {
		n = m
	}
	var dp, sp unsafe.Pointer
	if dk == Array {
		dp = dst.ptr
	} else {
		dp = (*sliceHeader)(dst.ptr).Data
	}
	switch {
	case sk == Array:
		sp = src.ptr
	case sk == Slice:
		sp = (*sliceHeader)(src.ptr).Data
	default:
		sp = (*stringHeader)(src.ptr).Data
	}
	if n > 0 {
		memmove(dp, sp, uintptr(n)*de.Size())
	}
	return n
}

// Channels.

// MakeChan creates a new channel with the specified type and buffer size.
func MakeChan(typ Type, buffer int) Value {
	if typ.Kind() != Chan {
		panic("reflect.MakeChan of non-chan type")
	}
	if buffer < 0 {
		panic("reflect.MakeChan: negative buffer size")
	}
	if typ.ChanDir() != BothDir {
		panic("reflect.MakeChan: unidirectional channel type")
	}
	t := typ.common()
	ch := makechan(t.Elem(), buffer)
	return Value{t, ch, flag(Chan)}
}

// Send sends x on the channel v.
func (v Value) Send(x Value) {
	v.mustBe(Chan)
	v.mustBeExported()
	v.send(x, false)
}

// TrySend attempts to send x on the channel v but will not block.
func (v Value) TrySend(x Value) bool {
	v.mustBe(Chan)
	v.mustBeExported()
	return v.send(x, true)
}

func (v Value) send(x Value, nb bool) (selected bool) {
	tt := (*rtabi.ChanType)(unsafe.Pointer(v.typ_))
	if ChanDir(tt.Dir)&SendDir == 0 {
		panic("reflect: send on recv-only channel")
	}
	x.mustBeExported()
	x = x.assignTo("reflect.Value.Send", tt.Elem, nil)
	var p unsafe.Pointer
	if x.flag&flagIndir != 0 {
		p = x.ptr
	} else {
		p = unsafe.Pointer(&x.ptr)
	}
	return chansend(v.pointer(), p, !nb)
}

// Recv receives and returns a value from the channel v.
func (v Value) Recv() (x Value, ok bool) {
	v.mustBe(Chan)
	v.mustBeExported()
	return v.recv(false)
}

// TryRecv attempts to receive a value from the channel v but will not
// block.
func (v Value) TryRecv() (x Value, ok bool) {
	v.mustBe(Chan)
	v.mustBeExported()
	return v.recv(true)
}

func (v Value) recv(nb bool) (val Value, ok bool) {
	tt := (*rtabi.ChanType)(unsafe.Pointer(v.typ_))
	if ChanDir(tt.Dir)&RecvDir == 0 {
		panic("reflect: recv on send-only channel")
	}
	t := tt.Elem
	val = Value{t, nil, flag(t.Kind())}
	var p unsafe.Pointer
	if t.IfaceIndir() {
		p = unsafe_New(t)
		val.ptr = p
		val.flag |= flagIndir
	} else {
		p = unsafe.Pointer(&val.ptr)
	}
	selected, ok := chanrecv(v.pointer(), p, !nb)
	if !selected {
		val = Value{}
	}
	return
}

// Conversions.

// Convert returns the value v converted to type t.
func (v Value) Convert(t Type) Value {
	op := convertOp(t.common(), v.typ_)
	if op == nil {
		panic("reflect.Value.Convert: value of type " + v.typ_.String() + " cannot be converted to type " + t.String())
	}
	return op(v, t)
}

// CanConvert reports whether the value v can be converted to type t.
func (v Value) CanConvert(t Type) bool {
	vt := v.Type()
	if !vt.ConvertibleTo(t) {
		return false
	}
	switch {
	case vt.Kind() == Slice && t.Kind() == Array:
		if t.Len() > v.Len() {
			return false
		}
	case vt.Kind() == Slice && t.Kind() == Pointer && t.Elem().Kind() == Array:
		n := t.Elem().Len()
		if n > v.Len() {
			return false
		}
	}
	return true
}

// convertOp returns the function to convert a value of type src to a
// value of type dst, or nil.
func convertOp(dst, src *rtabi.Type) func(Value, Type) Value {
	switch Kind(src.Kind()) {
	case Int, Int8, Int16, Int32, Int64:
		switch Kind(dst.Kind()) {
		case Int, Int8, Int16, Int32, Int64, Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
			return cvtInt
		case Float32, Float64:
			return cvtIntFloat
		case String:
			return cvtIntString
		}
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		switch Kind(dst.Kind()) {
		case Int, Int8, Int16, Int32, Int64, Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
			return cvtUint
		case Float32, Float64:
			return cvtUintFloat
		case String:
			return cvtUintString
		}
	case Float32, Float64:
		switch Kind(dst.Kind()) {
		case Int, Int8, Int16, Int32, Int64:
			return cvtFloatInt
		case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
			return cvtFloatUint
		case Float32, Float64:
			return cvtFloat
		}
	case Complex64, Complex128:
		switch Kind(dst.Kind()) {
		case Complex64, Complex128:
			return cvtComplex
		}
	case String:
		if dst.Kind() == rtabi.Slice && dst.Elem().PkgPath() == "" {
			switch Kind(dst.Elem().Kind()) {
			case Uint8:
				return cvtStringBytes
			case Int32:
				return cvtStringRunes
			}
		}
	case Slice:
		if dst.Kind() == rtabi.String && src.Elem().PkgPath() == "" {
			switch Kind(src.Elem().Kind()) {
			case Uint8:
				return cvtBytesString
			case Int32:
				return cvtRunesString
			}
		}
		if dst.Kind() == rtabi.Pointer && dst.Elem().Kind() == rtabi.Array && src.Elem() == dst.Elem().Elem() {
			return cvtSliceArrayPtr
		}
		if dst.Kind() == rtabi.Array && src.Elem() == dst.Elem() {
			return cvtSliceArray
		}
	case Chan:
		if dst.Kind() == rtabi.Chan && specialChannelAssignability(dst, src) {
			return cvtDirect
		}
	}

	// dst and src have the same underlying type.
	if haveIdenticalUnderlyingType(dst, src, false) {
		return cvtDirect
	}

	// dst and src are non-defined pointer types with the same underlying
	// base type.
	if dst.Kind() == rtabi.Pointer && dst.Name() == "" &&
		src.Kind() == rtabi.Pointer && src.Name() == "" &&
		haveIdenticalUnderlyingType(dst.Elem(), src.Elem(), false) {
		return cvtDirect
	}

	if implements(dst, src) {
		if src.Kind() == rtabi.Interface {
			return cvtI2I
		}
		return cvtT2I
	}
	return nil
}

func makeInt(f flag, bits uint64, t Type) Value {
	typ := t.common()
	ptr := unsafe_New(typ)
	switch typ.Size() {
	case 1:
		*(*uint8)(ptr) = uint8(bits)
	case 2:
		*(*uint16)(ptr) = uint16(bits)
	case 4:
		*(*uint32)(ptr) = uint32(bits)
	case 8:
		*(*uint64)(ptr) = bits
	}
	return Value{typ, ptr, f | flagIndir | flag(typ.Kind())}
}

func makeFloat(f flag, v float64, t Type) Value {
	typ := t.common()
	ptr := unsafe_New(typ)
	switch typ.Size() {
	case 4:
		*(*float32)(ptr) = float32(v)
	case 8:
		*(*float64)(ptr) = v
	}
	return Value{typ, ptr, f | flagIndir | flag(typ.Kind())}
}

func makeComplex(f flag, v complex128, t Type) Value {
	typ := t.common()
	ptr := unsafe_New(typ)
	switch typ.Size() {
	case 8:
		*(*complex64)(ptr) = complex64(v)
	case 16:
		*(*complex128)(ptr) = v
	}
	return Value{typ, ptr, f | flagIndir | flag(typ.Kind())}
}

func makeString(f flag, v string, t Type) Value {
	ret := New(t).Elem()
	ret.SetString(v)
	ret.flag = ret.flag&^flagAddr | f
	return ret
}

func makeBytes(f flag, v []byte, t Type) Value {
	ret := New(t).Elem()
	ret.SetBytes(v)
	ret.flag = ret.flag&^flagAddr | f
	return ret
}

func makeRunes(f flag, v []rune, t Type) Value {
	ret := New(t).Elem()
	ret.setRunes(v)
	ret.flag = ret.flag&^flagAddr | f
	return ret
}

func cvtInt(v Value, t Type) Value  { return makeInt(v.flag.ro(), uint64(v.Int()), t) }
func cvtUint(v Value, t Type) Value { return makeInt(v.flag.ro(), v.Uint(), t) }

func cvtFloatInt(v Value, t Type) Value {
	return makeInt(v.flag.ro(), uint64(int64(v.Float())), t)
}

func cvtFloatUint(v Value, t Type) Value {
	return makeInt(v.flag.ro(), uint64(v.Float()), t)
}

func cvtIntFloat(v Value, t Type) Value {
	return makeFloat(v.flag.ro(), float64(v.Int()), t)
}

func cvtUintFloat(v Value, t Type) Value {
	return makeFloat(v.flag.ro(), float64(v.Uint()), t)
}

func cvtFloat(v Value, t Type) Value {
	if v.Type().Kind() == Float32 && t.Kind() == Float32 {
		// Keep the exact bits (NaN payloads).
		return makeFloat(v.flag.ro(), float64(*(*float32)(v.ptr)), t)
	}
	return makeFloat(v.flag.ro(), v.Float(), t)
}

func cvtComplex(v Value, t Type) Value {
	return makeComplex(v.flag.ro(), v.Complex(), t)
}

func cvtIntString(v Value, t Type) Value {
	s := "�"
	if x := v.Int(); int64(rune(x)) == x {
		s = string(rune(x))
	}
	return makeString(v.flag.ro(), s, t)
}

func cvtUintString(v Value, t Type) Value {
	s := "�"
	if x := v.Uint(); uint64(rune(x)) == x {
		s = string(rune(x))
	}
	return makeString(v.flag.ro(), s, t)
}

func cvtBytesString(v Value, t Type) Value {
	return makeString(v.flag.ro(), string(v.Bytes()), t)
}

func cvtStringBytes(v Value, t Type) Value {
	return makeBytes(v.flag.ro(), []byte(v.String()), t)
}

func cvtRunesString(v Value, t Type) Value {
	return makeString(v.flag.ro(), string(v.runes()), t)
}

func cvtStringRunes(v Value, t Type) Value {
	return makeRunes(v.flag.ro(), []rune(v.String()), t)
}

func cvtSliceArrayPtr(v Value, t Type) Value {
	n := t.Elem().Len()
	if n > v.Len() {
		panic("reflect: cannot convert slice with length " + strconv.Itoa(v.Len()) + " to pointer to array with length " + strconv.Itoa(n))
	}
	h := (*sliceHeader)(v.ptr)
	return Value{t.common(), h.Data, v.flag&^(flagIndir|flagAddr|flagKindMask) | flag(Pointer)}
}

func cvtSliceArray(v Value, t Type) Value {
	n := t.Len()
	if n > v.Len() {
		panic("reflect: cannot convert slice with length " + strconv.Itoa(v.Len()) + " to array with length " + strconv.Itoa(n))
	}
	h := (*sliceHeader)(v.ptr)
	typ := t.common()
	ptr := h.Data
	c := unsafe_New(typ)
	typedmemmove(typ, c, ptr)
	ptr = c
	return Value{typ, ptr, v.flag&^(flagAddr|flagKindMask) | flag(Array)}
}

func cvtDirect(v Value, typ Type) Value {
	f := v.flag
	if f&flagAddr != 0 {
		// Indirect, mutable word: make a copy.
		ptr := unsafe_New(typ.common())
		typedmemmove(typ.common(), ptr, v.ptr)
		v.ptr = ptr
		f &^= flagAddr
	}
	return Value{typ.common(), v.ptr, v.flag.ro() | f}
}

func cvtT2I(v Value, typ Type) Value {
	target := unsafe_New(typ.common())
	x := valueInterface(v, false)
	if typ.NumMethod() == 0 {
		*(*any)(target) = x
	} else {
		e := (*rtabi.EmptyInterface)(unsafe.Pointer(&x))
		ni := (*rtabi.NonEmptyInterface)(target)
		ni.ITab = assertE2I((*rtabi.InterfaceType)(unsafe.Pointer(typ.common())), e.Type)
		ni.Data = e.Data
	}
	return Value{typ.common(), target, v.flag.ro() | flagIndir | flag(Interface)}
}

func cvtI2I(v Value, typ Type) Value {
	if v.IsNil() {
		ret := Zero(typ)
		ret.flag |= v.flag.ro()
		return ret
	}
	return cvtT2I(v.Elem(), typ)
}

// OverflowInt reports whether the int64 x cannot be represented by v's
// type.
func (v Value) OverflowInt(x int64) bool { return v.Type().OverflowInt(x) }

// OverflowUint reports whether the uint64 x cannot be represented by v's
// type.
func (v Value) OverflowUint(x uint64) bool { return v.Type().OverflowUint(x) }

// OverflowFloat reports whether the float64 x cannot be represented by
// v's type.
func (v Value) OverflowFloat(x float64) bool { return v.Type().OverflowFloat(x) }

// OverflowComplex reports whether the complex128 x cannot be represented
// by v's type.
func (v Value) OverflowComplex(x complex128) bool { return v.Type().OverflowComplex(x) }

// Swapper returns a function that swaps the elements in the provided
// slice.
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
