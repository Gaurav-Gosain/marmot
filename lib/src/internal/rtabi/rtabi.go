// Package rtabi mirrors the marmot type descriptor layout of
// runtime/type.go with exported names, for reflect and internal/reflectlite.
// The layouts must stay identical to the runtime's; lib/hosttest/typecheck
// checks every field offset.
package rtabi

import "unsafe"

// Kind values (reflect.Kind).
type Kind uint8

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

// TFlag bits.
const (
	TFlagNamed         = 1 << 0
	TFlagComparable    = 1 << 1
	TFlagRegularMemory = 1 << 2
	TFlagDirectIface   = 1 << 3
)

// Type is runtime._type.
type Type struct {
	Size_      uintptr
	PtrBytes   uintptr
	GCData     *byte
	Str        string
	Uncommon_  *UncommonType
	Kind_      Kind
	Align_     uint8
	FieldAlign uint8
	TFlag      uint8
}

// UncommonType is runtime.uncommontype.
type UncommonType struct {
	PkgPath   string
	Methods   *Method
	Mcount    uint32
	Xcount    uint32
	PtrToThis *Type
}

// Method is runtime.method.
type Method struct {
	Name string
	Mtyp *Type
	Ifn  unsafe.Pointer
}

// PtrType is runtime.ptrtype.
type PtrType struct {
	Type
	Elem *Type
}

// SliceType is runtime.slicetype.
type SliceType struct {
	Type
	Elem *Type
}

// ArrayType is runtime.arraytype.
type ArrayType struct {
	Type
	Elem *Type
	Len  uintptr
}

// ChanType is runtime.chantype.
type ChanType struct {
	Type
	Elem *Type
	Dir  uintptr
}

// MapType is runtime.maptype.
type MapType struct {
	Type
	Key  *Type
	Elem *Type
}

// FuncType is runtime.functype.
type FuncType struct {
	Type
	Params   **Type
	InCount  uint16
	OutCount uint16
	Variadic uint8
}

// Imethod is runtime.imethod.
type Imethod struct {
	Name string
	Ityp *Type
}

// InterfaceType is runtime.interfacetype.
type InterfaceType struct {
	Type
	PkgPath string
	Methods *Imethod
	Mcount  uintptr
}

// StructField is runtime.structfield.
type StructField struct {
	Name        string
	Tag         string
	Typ         *Type
	OffsetEmbed uintptr
}

// StructType is runtime.structtype.
type StructType struct {
	Type
	PkgPath string
	Fields  *StructField
	NFields uintptr
}

// Itab is runtime.itab (fun has Mcount entries).
type Itab struct {
	Inter *InterfaceType
	Typ   *Type
	Hash  uint32
	Flags uint32
	Fun   [1]unsafe.Pointer
}

// EmptyInterface is the layout of interface{}.
type EmptyInterface struct {
	Type *Type
	Data unsafe.Pointer
}

// NonEmptyInterface is the layout of an interface with methods.
type NonEmptyInterface struct {
	ITab *Itab
	Data unsafe.Pointer
}

// Accessors.

func (t *Type) Kind() Kind              { return t.Kind_ }
func (t *Type) Size() uintptr           { return t.Size_ }
func (t *Type) Align() int              { return int(t.Align_) }
func (t *Type) Pointers() bool          { return t.PtrBytes != 0 }
func (t *Type) IfaceIndir() bool        { return t.TFlag&TFlagDirectIface == 0 }
func (t *Type) Comparable() bool        { return t.TFlag&TFlagComparable != 0 }
func (t *Type) HasName() bool           { return t.TFlag&TFlagNamed != 0 }
func (t *Type) Uncommon() *UncommonType { return t.Uncommon_ }
func (t *Type) String() string          { return t.Str }

// Name returns the name of a defined type ("" otherwise).
func (t *Type) Name() string {
	if !t.HasName() {
		return ""
	}
	s := t.Str
	i := len(s) - 1
	sqBrackets := 0
	for i >= 0 && (s[i] != '.' || sqBrackets != 0) {
		switch s[i] {
		case ']':
			sqBrackets++
		case '[':
			sqBrackets--
		}
		i--
	}
	return s[i+1:]
}

// PkgPath returns the package path of a defined type.
func (t *Type) PkgPath() string {
	if !t.HasName() || t.Uncommon_ == nil {
		return ""
	}
	return t.Uncommon_.PkgPath
}

// Elem returns the element type of pointer, slice, array, chan and map
// types, nil otherwise.
func (t *Type) Elem() *Type {
	switch t.Kind_ {
	case Pointer:
		return (*PtrType)(unsafe.Pointer(t)).Elem
	case Slice:
		return (*SliceType)(unsafe.Pointer(t)).Elem
	case Array:
		return (*ArrayType)(unsafe.Pointer(t)).Elem
	case Chan:
		return (*ChanType)(unsafe.Pointer(t)).Elem
	case Map:
		return (*MapType)(unsafe.Pointer(t)).Elem
	}
	return nil
}

// Key returns the key type of a map type.
func (t *Type) Key() *Type {
	if t.Kind_ != Map {
		return nil
	}
	return (*MapType)(unsafe.Pointer(t)).Key
}

// Len returns the length of an array type.
func (t *Type) Len() int {
	if t.Kind_ != Array {
		return 0
	}
	return int((*ArrayType)(unsafe.Pointer(t)).Len)
}

// NumMethod returns the number of exported methods (interface methods for
// interface types).
func (t *Type) NumMethod() int {
	if t.Kind_ == Interface {
		return int((*InterfaceType)(unsafe.Pointer(t)).Mcount)
	}
	if t.Uncommon_ == nil {
		return 0
	}
	return int(t.Uncommon_.Xcount)
}

// MethodAt returns method i of the method set of t.
func (u *UncommonType) MethodAt(i int) *Method {
	return (*Method)(unsafe.Add(unsafe.Pointer(u.Methods), uintptr(i)*unsafe.Sizeof(Method{})))
}

// MethodAt returns method i of the interface.
func (it *InterfaceType) MethodAt(i int) *Imethod {
	return (*Imethod)(unsafe.Add(unsafe.Pointer(it.Methods), uintptr(i)*unsafe.Sizeof(Imethod{})))
}

// Field returns field i of the struct.
func (st *StructType) Field(i int) *StructField {
	return (*StructField)(unsafe.Add(unsafe.Pointer(st.Fields), uintptr(i)*unsafe.Sizeof(StructField{})))
}

// Offset returns the byte offset of the field.
func (f *StructField) Offset() uintptr { return f.OffsetEmbed >> 1 }

// Embedded reports whether the field is embedded.
func (f *StructField) Embedded() bool { return f.OffsetEmbed&1 != 0 }

// Param returns parameter (i < InCount) or result (InCount + j) i.
func (ft *FuncType) Param(i int) *Type {
	return *(**Type)(unsafe.Add(unsafe.Pointer(ft.Params), uintptr(i)*unsafe.Sizeof(uintptr(0))))
}

// IsExported reports whether a method or field name is exported. Unexported
// method names are qualified with their package path.
func IsExported(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' || len(name) > 0 && name[0] >= 0x80 && !hasDot(name)
}

func hasDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}

// ShortName strips the package qualification from a method name.
func ShortName(s string) string {
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

// Implements reports whether type t implements interface type it. Both
// method lists are sorted by name.
func Implements(it *InterfaceType, t *Type) bool {
	ni := int(it.Mcount)
	if ni == 0 {
		return true
	}
	if t.Kind_ == Interface {
		tt := (*InterfaceType)(unsafe.Pointer(t))
		j := 0
		for k := 0; k < ni; k++ {
			im := it.MethodAt(k)
			for ; j < int(tt.Mcount); j++ {
				tm := tt.MethodAt(j)
				if tm.Name == im.Name && tm.Ityp == im.Ityp {
					break
				}
			}
			if j == int(tt.Mcount) {
				return false
			}
		}
		return true
	}
	u := t.Uncommon_
	if u == nil {
		return false
	}
	j := 0
	for k := 0; k < ni; k++ {
		im := it.MethodAt(k)
		for ; j < int(u.Mcount); j++ {
			tm := u.MethodAt(j)
			if tm.Name == im.Name && tm.Mtyp == im.Ityp {
				break
			}
		}
		if j == int(u.Mcount) {
			return false
		}
	}
	return true
}

// TypeOf returns the type word of an interface{} value.
func TypeOf(a any) *Type {
	return (*EmptyInterface)(unsafe.Pointer(&a)).Type
}

// Hiter is runtime.hiter, the map iterator.
type Hiter struct {
	Key      unsafe.Pointer
	Elem     unsafe.Pointer
	T        unsafe.Pointer
	M        unsafe.Pointer
	Groups   unsafe.Pointer
	Mask     uintptr
	Start    uintptr
	I        uintptr
	ClearSeq uint32
}

// Scase is runtime.scase.
type Scase struct {
	C    unsafe.Pointer
	Elem unsafe.Pointer
	Send bool
}
