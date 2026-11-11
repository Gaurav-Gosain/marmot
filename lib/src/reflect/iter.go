package reflect

import (
	"iter"
	"unsafe"
)

// Fields returns an iterator over the fields of the struct type t.
func (t *rtype) Fields() iter.Seq[StructField] {
	if t.Kind() != Struct {
		panic("reflect: Fields of non-struct type " + t.String())
	}
	return func(yield func(StructField) bool) {
		for i := range t.NumField() {
			if !yield(t.Field(i)) {
				return
			}
		}
	}
}

// Methods returns an iterator over the methods of t.
func (t *rtype) Methods() iter.Seq[Method] {
	return func(yield func(Method) bool) {
		for i := range t.NumMethod() {
			if !yield(t.Method(i)) {
				return
			}
		}
	}
}

// Ins returns an iterator over the parameter types of the func type t.
func (t *rtype) Ins() iter.Seq[Type] {
	if t.Kind() != Func {
		panic("reflect: Ins of non-func type " + t.String())
	}
	return func(yield func(Type) bool) {
		for i := range t.NumIn() {
			if !yield(t.In(i)) {
				return
			}
		}
	}
}

// Outs returns an iterator over the result types of the func type t.
func (t *rtype) Outs() iter.Seq[Type] {
	if t.Kind() != Func {
		panic("reflect: Outs of non-func type " + t.String())
	}
	return func(yield func(Type) bool) {
		for i := range t.NumOut() {
			if !yield(t.Out(i)) {
				return
			}
		}
	}
}

// Fields returns an iterator over the fields of the struct v with their
// values.
func (v Value) Fields() iter.Seq2[StructField, Value] {
	t := v.Type()
	if t.Kind() != Struct {
		panic("reflect: Fields of non-struct type " + t.String())
	}
	return func(yield func(StructField, Value) bool) {
		for i := range v.NumField() {
			if !yield(t.Field(i), v.Field(i)) {
				return
			}
		}
	}
}

// Methods returns an iterator over the methods of v. Method values are not
// supported by marmot, so iterating panics when there is a method.
func (v Value) Methods() iter.Seq2[Method, Value] {
	rt := v.Type()
	n := v.NumMethod()
	return func(yield func(Method, Value) bool) {
		for i := range n {
			if !yield(rt.Method(i), v.Method(i)) {
				return
			}
		}
	}
}

// Seq returns an iterator that loops over the elements of v as a range
// loop would (range-over-func values are not supported by marmot).
func (v Value) Seq() iter.Seq[Value] {
	switch v.Kind() {
	case Int, Int8, Int16, Int32, Int64:
		return func(yield func(Value) bool) {
			n := v.Int()
			for i := int64(0); i < n; i++ {
				if !yield(ValueOf(i).Convert(v.Type())) {
					return
				}
			}
		}
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		return func(yield func(Value) bool) {
			n := v.Uint()
			for i := uint64(0); i < n; i++ {
				if !yield(ValueOf(i).Convert(v.Type())) {
					return
				}
			}
		}
	case Pointer:
		if v.Elem().Kind() != Array {
			break
		}
		return func(yield func(Value) bool) {
			v = v.Elem()
			for i := range v.Len() {
				if !yield(ValueOf(i)) {
					return
				}
			}
		}
	case Array, Slice:
		return func(yield func(Value) bool) {
			for i := range v.Len() {
				if !yield(ValueOf(i)) {
					return
				}
			}
		}
	case String:
		return func(yield func(Value) bool) {
			for i := range v.String() {
				if !yield(ValueOf(i)) {
					return
				}
			}
		}
	case Map:
		return func(yield func(Value) bool) {
			i := v.MapRange()
			for i.Next() {
				if !yield(i.Key()) {
					return
				}
			}
		}
	case Chan:
		return func(yield func(Value) bool) {
			for value, ok := v.Recv(); ok; value, ok = v.Recv() {
				if !yield(value) {
					return
				}
			}
		}
	}
	panic("reflect: " + v.Type().String() + " cannot produce iter.Seq[Value]")
}

// Seq2 returns an iterator that loops over the elements of v as a range
// loop with two variables would.
func (v Value) Seq2() iter.Seq2[Value, Value] {
	switch v.Kind() {
	case Pointer:
		if v.Elem().Kind() != Array {
			break
		}
		return func(yield func(Value, Value) bool) {
			v = v.Elem()
			for i := range v.Len() {
				if !yield(ValueOf(i), v.Index(i)) {
					return
				}
			}
		}
	case Array, Slice:
		return func(yield func(Value, Value) bool) {
			for i := range v.Len() {
				if !yield(ValueOf(i), v.Index(i)) {
					return
				}
			}
		}
	case String:
		return func(yield func(Value, Value) bool) {
			for i, v := range v.String() {
				if !yield(ValueOf(i), ValueOf(v)) {
					return
				}
			}
		}
	case Map:
		return func(yield func(Value, Value) bool) {
			i := v.MapRange()
			for i.Next() {
				if !yield(i.Key(), i.Value()) {
					return
				}
			}
		}
	}
	panic("reflect: " + v.Type().String() + " cannot produce iter.Seq2[Value, Value]")
}

// TypeAssert is semantically equivalent to v2, ok := v.Interface().(T).
func TypeAssert[T any](v Value) (T, bool) {
	if v.flag == 0 {
		panic(&ValueError{"reflect.TypeAssert", Invalid})
	}
	if v.flag&flagRO != 0 {
		panic("reflect.TypeAssert: cannot return value obtained from unexported field or method")
	}
	out, ok := valueInterface(v, false).(T)
	return out, ok
}

// SliceAt returns a Value representing a slice whose underlying data
// starts at p, with length and capacity equal to n.
func SliceAt(typ Type, p unsafe.Pointer, n int) Value {
	st := sliceOfElem(typ.common())
	if st == nil {
		panic("reflect.SliceAt: slice type of " + typ.String() + " not available in marmot")
	}
	s := sliceHeader{Data: p, Len: n, Cap: n}
	return Value{st, unsafe.Pointer(&s), flagIndir | flag(Slice)}
}

// InterfaceData returns a pair of unspecified uintptr values.
func (v Value) InterfaceData() [2]uintptr {
	v.mustBe(Interface)
	return *(*[2]uintptr)(v.ptr)
}

// StringHeader is the runtime representation of a string.
type StringHeader struct {
	Data uintptr
	Len  int
}

// SliceHeader is the runtime representation of a slice.
type SliceHeader struct {
	Data uintptr
	Len  int
	Cap  int
}
