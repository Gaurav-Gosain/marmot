package main

type A struct {
	Bool    bool
	Int     int
	Int8    int8
	Int16   int16
	Int32   int32
	Int64   int64
	Uint    uint
	Uint8   uint8
	Uint16  uint16
	Uint32  uint32
	Uint64  uint64
	Uintptr uintptr
	Float32 float32
	Float64 float64
	Foo     string
	P       *int
}

var a = A{Bool: true, Int: 2, Int8: 3, Int16: 4, Int32: 5, Int64: 6, Uint: 7, Uint8: 8, Uint16: 9, Uint32: 10, Uint64: 11, Uintptr: 12, Float32: 14.1, Float64: 15.1, Foo: "foo"}
var b = A{P: &a.Int}

func main() { println(a.Bool, a.Int, a.Int8, a.Int16, a.Int32, a.Int64, a.Uint, a.Uint8, a.Uint16, a.Uint32, a.Uint64, a.Uintptr, a.Float32, a.Float64, a.Foo, b.P == nil) }
