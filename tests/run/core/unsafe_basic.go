package main

import "unsafe"

type S struct {
	a int8
	b int64
	c int16
	d string
	e []int
	f interface{}
}

func main() {
	var s S
	println(unsafe.Sizeof(s), unsafe.Alignof(s), unsafe.Sizeof(s.a), unsafe.Alignof(s.b))
	println(unsafe.Offsetof(s.b), unsafe.Offsetof(s.c), unsafe.Offsetof(s.d), unsafe.Offsetof(s.e), unsafe.Offsetof(s.f))
	println(unsafe.Sizeof(int(0)), unsafe.Sizeof(uintptr(0)), unsafe.Sizeof(""), unsafe.Sizeof([]int{}), unsafe.Sizeof(map[int]int{}))
	println(unsafe.Sizeof(true), unsafe.Sizeof(float32(0)), unsafe.Sizeof(complex128(0)), unsafe.Sizeof([3]int16{}))
	x := [4]int32{1, 2, 3, 4}
	p := unsafe.Pointer(&x[0])
	p2 := (*int32)(unsafe.Add(p, 8))
	println(*p2)
	p3 := (*int32)(unsafe.Pointer(uintptr(p) + 4))
	println(*p3)
	f := 1.0
	bits := *(*uint64)(unsafe.Pointer(&f))
	println(bits)
	str := "hello"
	b := unsafe.Slice(unsafe.StringData(str), len(str))
	println(len(b), b[1])
	sl := []int{1, 2, 3}
	arr := unsafe.Slice(&sl[0], 2)
	println(len(arr), arr[1], unsafe.SliceData(sl) == &sl[0])
	bs := []byte("abc")
	println(unsafe.String(&bs[0], 2))
}
