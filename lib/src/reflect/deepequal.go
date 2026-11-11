package reflect

import (
	"internal/rtabi"
	"unsafe"
)

// During deepValueEqual, keep track of checks that are in progress, to
// stop at cycles.
type visit struct {
	a1  unsafe.Pointer
	a2  unsafe.Pointer
	typ *rtabi.Type
}

// deepValueEqual tests for deep equality using reflected types. The map
// argument tracks comparisons that have already been seen.
func deepValueEqual(v1, v2 Value, visited map[visit]bool) bool {
	if !v1.IsValid() || !v2.IsValid() {
		return v1.IsValid() == v2.IsValid()
	}
	if v1.typ_ != v2.typ_ {
		return false
	}

	hard := func(v1, v2 Value) bool {
		switch v1.Kind() {
		case Pointer, Map, Slice, Interface:
			return !v1.IsNil()
		}
		return false
	}

	if hard(v1, v2) {
		var addr1, addr2 unsafe.Pointer
		switch v1.Kind() {
		case Pointer, Map:
			addr1, addr2 = v1.pointer(), v2.pointer()
		default:
			addr1, addr2 = v1.ptr, v2.ptr
		}
		if uintptr(addr1) > uintptr(addr2) {
			addr1, addr2 = addr2, addr1
		}
		typ := v1.typ_
		v := visit{addr1, addr2, typ}
		if visited[v] {
			return true
		}
		visited[v] = true
	}

	switch v1.Kind() {
	case Array:
		for i := 0; i < v1.Len(); i++ {
			if !deepValueEqual(v1.Index(i), v2.Index(i), visited) {
				return false
			}
		}
		return true
	case Slice:
		if v1.IsNil() != v2.IsNil() {
			return false
		}
		if v1.Len() != v2.Len() {
			return false
		}
		if v1.UnsafePointer() == v2.UnsafePointer() {
			return true
		}
		for i := 0; i < v1.Len(); i++ {
			if !deepValueEqual(v1.Index(i), v2.Index(i), visited) {
				return false
			}
		}
		return true
	case Interface:
		if v1.IsNil() || v2.IsNil() {
			return v1.IsNil() == v2.IsNil()
		}
		return deepValueEqual(v1.Elem(), v2.Elem(), visited)
	case Pointer:
		if v1.UnsafePointer() == v2.UnsafePointer() {
			return true
		}
		return deepValueEqual(v1.Elem(), v2.Elem(), visited)
	case Struct:
		for i, n := 0, v1.NumField(); i < n; i++ {
			if !deepValueEqual(v1.Field(i), v2.Field(i), visited) {
				return false
			}
		}
		return true
	case Map:
		if v1.IsNil() != v2.IsNil() {
			return false
		}
		if v1.Len() != v2.Len() {
			return false
		}
		if v1.UnsafePointer() == v2.UnsafePointer() {
			return true
		}
		iter := v1.MapRange()
		for iter.Next() {
			val1 := iter.Value()
			val2 := v2.MapIndex(iter.Key())
			if !val1.IsValid() || !val2.IsValid() || !deepValueEqual(val1, val2, visited) {
				return false
			}
		}
		return true
	case Func:
		if v1.IsNil() && v2.IsNil() {
			return true
		}
		// Can't do better than this.
		return false
	case Int, Int8, Int16, Int32, Int64:
		return v1.Int() == v2.Int()
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		return v1.Uint() == v2.Uint()
	case String:
		return v1.String() == v2.String()
	case Bool:
		return v1.Bool() == v2.Bool()
	case Float32, Float64:
		return v1.Float() == v2.Float()
	case Complex64, Complex128:
		return v1.Complex() == v2.Complex()
	case Chan, UnsafePointer:
		return v1.pointer() == v2.pointer()
	}
	// Normal equality suffices.
	return valueInterface(v1, false) == valueInterface(v2, false)
}

// DeepEqual reports whether x and y are "deeply equal", as defined by the
// gc reflect package.
func DeepEqual(x, y any) bool {
	if x == nil || y == nil {
		return x == y
	}
	v1 := ValueOf(x)
	v2 := ValueOf(y)
	if v1.Type() != v2.Type() {
		return false
	}
	return deepValueEqual(v1, v2, make(map[visit]bool))
}

// A SelectDir describes the communication direction of a select case.
type SelectDir int

const (
	_             SelectDir = iota
	SelectSend              // case Chan <- Send
	SelectRecv              // case <-Chan:
	SelectDefault           // default
)

// A SelectCase describes a single case in a select operation.
type SelectCase struct {
	Dir  SelectDir
	Chan Value
	Send Value
}

// Select executes a select operation described by the list of cases.
func Select(cases []SelectCase) (chosen int, recv Value, recvOK bool) {
	rc := make([]rtabi.Scase, 0, len(cases))
	idx := make([]int, 0, len(cases))
	recvVals := make([]Value, len(cases))
	hasDefault := false
	defaultIdx := -1
	for i, c := range cases {
		switch c.Dir {
		case SelectDefault:
			if hasDefault {
				panic("reflect.Select: multiple default cases")
			}
			hasDefault = true
			defaultIdx = i
		case SelectSend:
			ch := c.Chan
			if !ch.IsValid() {
				continue
			}
			ch.mustBe(Chan)
			tt := (*rtabi.ChanType)(unsafe.Pointer(ch.typ_))
			v := c.Send.assignTo("reflect.Select", tt.Elem, nil)
			var p unsafe.Pointer
			if v.flag&flagIndir != 0 {
				p = v.ptr
			} else {
				p = unsafe.Pointer(&v.ptr)
			}
			rc = append(rc, rtabi.Scase{C: ch.pointer(), Elem: p, Send: true})
			idx = append(idx, i)
		case SelectRecv:
			ch := c.Chan
			if !ch.IsValid() {
				continue
			}
			ch.mustBe(Chan)
			tt := (*rtabi.ChanType)(unsafe.Pointer(ch.typ_))
			t := tt.Elem
			rv := Value{t, nil, flag(t.Kind())}
			var p unsafe.Pointer
			if t.IfaceIndir() {
				p = unsafe_New(t)
				rv.ptr = p
				rv.flag |= flagIndir
			} else {
				p = unsafe_New(ptrTo(t))
			}
			recvVals[i] = rv
			rc = append(rc, rtabi.Scase{C: ch.pointer(), Elem: p})
			idx = append(idx, i)
		default:
			panic("reflect.Select: invalid Dir")
		}
	}
	var sc *rtabi.Scase
	if len(rc) > 0 {
		sc = &rc[0]
	}
	ci, ok := selectgo(sc, len(rc), !hasDefault)
	if ci < 0 {
		return defaultIdx, Value{}, false
	}
	chosen = idx[ci]
	if cases[chosen].Dir == SelectRecv {
		rv := recvVals[chosen]
		if rv.flag&flagIndir == 0 {
			rv.ptr = *(*unsafe.Pointer)(rc[ci].Elem)
		}
		return chosen, rv, ok
	}
	return chosen, Value{}, false
}

// VisibleFields returns all the visible fields in t, which must be a
// struct type, in the order of the gc implementation (breadth first by
// depth, then by index).
func VisibleFields(t Type) []StructField {
	if t == nil {
		panic("reflect: VisibleFields(nil)")
	}
	if t.Kind() != Struct {
		panic("reflect.VisibleFields of non-struct type")
	}
	var out []StructField
	var walk func(t Type, index []int, depth int)
	seen := map[Type]bool{}
	walk = func(t Type, index []int, depth int) {
		if seen[t] {
			return
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			f.Index = append(append([]int(nil), index...), i)
			out = append(out, f)
			if f.Anonymous {
				ft := f.Type
				if ft.Kind() == Pointer {
					ft = ft.Elem()
				}
				if ft.Kind() == Struct {
					walk(ft, f.Index, depth+1)
				}
			}
		}
	}
	walk(t, nil, 0)
	return out
}
