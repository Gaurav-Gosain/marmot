// Type descriptors.
//
// The compiler emits one descriptor per Go type that needs one (types that
// are converted to interfaces, allocated with new/make, used as map or chan
// elements, and so on). Descriptors are constant data with linkonce_odr
// linkage and a canonical symbol name, so two descriptors for the same Go
// type are merged by the linker. The runtime relies on that: type identity is
// pointer identity of *_type.
//
// Every descriptor starts with a _type header. Kinds that need more data use
// a larger struct whose first field is the _type header (ptrtype, slicetype,
// arraytype, chantype, maptype, functype, interfacetype, structtype). The
// compiler must emit exactly these layouts; field order and sizes are part
// of the compiler/runtime contract. All strings are ordinary Go strings
// {ptr, len} pointing at constant data.

package runtime

import "unsafe"

// Kind values. They match reflect.Kind.
const (
	kindInvalid = iota
	kindBool
	kindInt
	kindInt8
	kindInt16
	kindInt32
	kindInt64
	kindUint
	kindUint8
	kindUint16
	kindUint32
	kindUint64
	kindUintptr
	kindFloat32
	kindFloat64
	kindComplex64
	kindComplex128
	kindArray
	kindChan
	kindFunc
	kindInterface
	kindMap
	kindPointer
	kindSlice
	kindString
	kindStruct
	kindUnsafePointer
)

// tflag bits.
const (
	// tflagNamed: the type has a name (defined type or predeclared type).
	// str is then "pkgname.Name" for defined types ("int" for predeclared).
	tflagNamed = 1 << 0
	// tflagComparable: values of the type can be compared with == (and so
	// hashed). Clear for slices, maps, funcs and aggregates containing them.
	tflagComparable = 1 << 1
	// tflagRegularMemory: comparable, and equality and hashing are plain
	// byte comparison over size bytes. True only when the type has no
	// padding, no floats, no complex numbers, no strings and no interfaces
	// anywhere inside it. Bool, integers, pointers, chans, unsafe.Pointer
	// and arrays/structs made only of those (without padding) qualify.
	tflagRegularMemory = 1 << 2
	// tflagDirectIface: a value of this type is pointer shaped and is stored
	// directly in the data word of an interface instead of being boxed.
	// Set for pointers, maps, chans, funcs, unsafe.Pointer, and structs or
	// arrays of exactly one element whose only element is pointer shaped.
	tflagDirectIface = 1 << 3
)

// _type is the common header of every type descriptor. 56 bytes on 64 bit
// targets (the last word holds kind, align, fieldAlign, tflag and padding).
type _type struct {
	// size is the size of a value in bytes (Go's unsafe.Sizeof).
	size uintptr
	// ptrdata is the number of leading bytes of a value that can contain
	// pointers. 0 means the type contains no pointers at all (noscan).
	ptrdata uintptr
	// gcdata is the pointer bitmap of one value: bit i (byte i/8, bit i%8,
	// least significant bit first) is set when the pointer-sized word i
	// holds a pointer. The bitmap covers all size/ptrsize words of the
	// value (bits past ptrdata are 0), rounded up to a whole byte, as
	// rt_alloc's precise mode reads it. nil with ptrdata != 0 asks the
	// collector to scan
	// values of this type conservatively; the compiler may do that for very
	// large types to keep the binary small.
	gcdata *byte
	// str is the type as Go prints it: "int", "main.T", "*main.T",
	// "[]string", "map[string]int", "func(int) error", "struct { X int }".
	// Used in panic messages and by reflect.
	str string
	// uncommon is non-nil for defined types and for types with methods.
	uncommon *uncommontype
	// kind is one of the kind constants above.
	kind uint8
	// align is the alignment of a value in memory (Go's unsafe.Alignof).
	align uint8
	// fieldAlign is the alignment as a struct field (same as align on all
	// supported targets).
	fieldAlign uint8
	// tflag is a set of tflag bits.
	tflag uint8
}

// uncommontype carries the name parts and the method set of a type.
type uncommontype struct {
	// pkgpath is the import path of the package that defines the type, ""
	// for predeclared and unnamed types.
	pkgpath string
	// methods points at mcount entries sorted by name (byte order). For a
	// type T it is the method set of T; for *T it is the method set of *T
	// (including the value receiver methods of T).
	methods *method
	mcount  uint32
	// xcount is the number of exported methods. Exported methods sort
	// before unexported ones because their names start with an upper case
	// letter and unexported names are qualified (see method.name).
	xcount uint32
	// ptrToThis is the descriptor of *T for a defined type T that is not
	// itself a pointer type, nil otherwise. reflect.New and
	// reflect.PointerTo use it so that their results have the canonical
	// descriptor.
	ptrToThis *_type
}

// method is one entry of a method set.
type method struct {
	// name is the method name. Unexported names are qualified with the
	// package path: "pkg/path.name". Exported names are bare: "Name".
	name string
	// mtyp is the signature without the receiver, a *functype.
	mtyp *_type
	// ifn is the code address used in itabs: a function whose first
	// parameter is the interface data word (the pointer itself for pointer
	// shaped types, a pointer to the boxed value otherwise), followed by
	// the method parameters. It takes no hidden closure parameter. nil if
	// the compiler proved the method is never called through an interface.
	ifn unsafe.Pointer
}

// ptrtype describes *elem.
type ptrtype struct {
	typ  _type
	elem *_type
}

// slicetype describes []elem.
type slicetype struct {
	typ  _type
	elem *_type
}

// arraytype describes [len]elem.
type arraytype struct {
	typ  _type
	elem *_type
	len  uintptr
}

// Channel directions for chantype.dir. They match reflect.ChanDir.
const (
	chanRecv = 1
	chanSend = 2
	chanBoth = 3
)

// chantype describes chan elem, <-chan elem, chan<- elem.
type chantype struct {
	typ  _type
	elem *_type
	dir  uintptr
}

// maptype describes map[key]elem. The table layout is private to the
// runtime and derived from key and elem when the map is made.
type maptype struct {
	typ  _type
	key  *_type
	elem *_type
}

// functype describes a func signature.
type functype struct {
	typ _type
	// params points at inCount parameter types followed by outCount
	// result types. nil when both counts are 0.
	params   **_type
	inCount  uint16
	outCount uint16
	// variadic is 1 when the last parameter is ...T (its type is []T).
	variadic uint8
}

// imethod is one method of an interface type.
type imethod struct {
	// name follows the same rule as method.name.
	name string
	// ityp is the signature, a *functype.
	ityp *_type
}

// interfacetype describes an interface. Methods are sorted by name in the
// same order as method sets, so itabs can be built with one merge pass.
type interfacetype struct {
	typ     _type
	pkgpath string
	methods *imethod
	mcount  uintptr
}

// structfield is one field of a struct type.
type structfield struct {
	// name is the field name. For an embedded field it is the type name
	// without the package ("T" for embedded *pkg.T).
	name string
	// tag is the raw struct tag, "" if none.
	tag string
	typ *_type
	// offsetEmbed is offset<<1 | embedded, where offset is the byte offset
	// of the field and embedded is 1 for an embedded field.
	offsetEmbed uintptr
}

// structtype describes a struct type.
type structtype struct {
	typ     _type
	pkgpath string
	fields  *structfield
	nfields uintptr
}

// itab is the type-or-itab word of a non-empty interface value.
// The compiler emits itabs as linkonce_odr constants when it knows the
// concrete type at the conversion; the runtime builds others in assertE2I.
// fun has one code address per interface method, in interfacetype method
// order (method.ifn values). The C struct is { inter, typ, hash, fun[N] };
// the Go declaration uses a length 1 array, and the runtime indexes past it.
type itab struct {
	inter *interfacetype
	typ   *_type
	// hash is a copy of a hash of typ used by type switches; the compiler
	// may emit 0, the runtime does not depend on it.
	hash uint32
	_    uint32
	fun  [1]unsafe.Pointer
}

// eface is the layout of interface{} (any).
type eface struct {
	typ  *_type
	data unsafe.Pointer
}

// iface is the layout of a non-empty interface.
type iface struct {
	tab  *itab
	data unsafe.Pointer
}

// slice is the layout of a slice value.
type slice struct {
	array unsafe.Pointer
	len   int
	cap   int
}

// stringStruct is the layout of a string value.
type stringStruct struct {
	str unsafe.Pointer
	len int
}

const ptrSize = unsafe.Sizeof(uintptr(0))

func efaceOf(p *any) *eface { return (*eface)(unsafe.Pointer(p)) }

func (t *_type) elemType() *_type {
	switch t.kind {
	case kindPointer:
		return (*ptrtype)(unsafe.Pointer(t)).elem
	case kindSlice:
		return (*slicetype)(unsafe.Pointer(t)).elem
	case kindArray:
		return (*arraytype)(unsafe.Pointer(t)).elem
	case kindChan:
		return (*chantype)(unsafe.Pointer(t)).elem
	case kindMap:
		return (*maptype)(unsafe.Pointer(t)).elem
	}
	return nil
}

func (t *_type) methodAt(i int) *method {
	u := t.uncommon
	return (*method)(unsafe.Add(unsafe.Pointer(u.methods), uintptr(i)*unsafe.Sizeof(method{})))
}

func (it *interfacetype) methodAt(i int) *imethod {
	return (*imethod)(unsafe.Add(unsafe.Pointer(it.methods), uintptr(i)*unsafe.Sizeof(imethod{})))
}

func (st *structtype) fieldAt(i int) *structfield {
	return (*structfield)(unsafe.Add(unsafe.Pointer(st.fields), uintptr(i)*unsafe.Sizeof(structfield{})))
}

func (f *structfield) offset() uintptr { return f.offsetEmbed >> 1 }

func (ft *functype) param(i int) *_type {
	return *(**_type)(unsafe.Add(unsafe.Pointer(ft.params), uintptr(i)*ptrSize))
}
