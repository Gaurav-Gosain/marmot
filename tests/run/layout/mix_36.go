package main

import (
	"fmt"
	"reflect"
	"sync"
	"unsafe"
)

var ints = [8]int{10, 11, 12, 13, 14, 15, 16, 17}

type N1 struct {
	a int8
	b int64
}

type N2 struct {
	x bool
	y [3]byte
	z float32
}

var _ = fmt.Sprint
var _ = reflect.TypeOf
var _ sync.Mutex
var _ unsafe.Pointer

type T struct {
	a2_0  [2]int16
	u32_1 uint32
	c64_2 complex64
	i16_3 int16
	up_4  uintptr
	i_5   int
	i8_6  int8
	n1_7  N1
	u32_8 uint32
	i_9   int
	z1_10 struct{}
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.a2_0, x.u32_1, x.c64_2, x.i16_3, x.up_4, x.i_5, x.i8_6, x.n1_7, x.u32_8, x.i_9, x.z1_10)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.a2_0, x.u32_1, x.c64_2, x.i16_3, x.up_4, x.i_5, x.i8_6, x.n1_7, x.u32_8, x.i_9, x.z1_10)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.a2_0, x.in.u32_1, x.in.c64_2, x.in.i16_3, x.in.up_4, x.in.i_5, x.in.i8_6, x.in.n1_7, x.in.u32_8, x.in.i_9, x.in.z1_10, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.a2_0, x.in.u32_1, x.in.c64_2, x.in.i16_3, x.in.up_4, x.in.i_5, x.in.i8_6, x.in.n1_7, x.in.u32_8, x.in.i_9, x.in.z1_10, x.post)
}

var gT = T{a2_0: [2]int16{-1, 2}, u32_1: 4000000000, c64_2: 1 + 2i, i16_3: -1234, up_4: 77, i_5: -42, i8_6: -5, n1_7: N1{3, -7}, u32_8: 4000000000, i_9: -42, z1_10: struct{}{}}
var gO = O{pre: 7, in: T{a2_0: [2]int16{-1, 2}, u32_1: 4000000000, c64_2: 1 + 2i, i16_3: -1234, up_4: 77, i_5: -42, i8_6: -5, n1_7: N1{3, -7}, u32_8: 4000000000, i_9: -42, z1_10: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{a2_0: [2]int16{-1, 2}, u32_1: 4000000000, c64_2: 1 + 2i, i16_3: -1234, up_4: 77, i_5: -42, i8_6: -5, n1_7: N1{3, -7}, u32_8: 4000000000, i_9: -42, z1_10: struct{}{}}, {}, T{a2_0: [2]int16{300, -300}, u32_1: 99, c64_2: -3 - 4i, i16_3: 4321, up_4: 88, i_5: 123456789, i8_6: 7, n1_7: N1{-3, 70000000000}, u32_8: 99, i_9: 123456789, z1_10: struct{}{}}}
var sT = []T{T{a2_0: [2]int16{300, -300}, u32_1: 99, c64_2: -3 - 4i, i16_3: 4321, up_4: 88, i_5: 123456789, i8_6: 7, n1_7: N1{-3, 70000000000}, u32_8: 99, i_9: 123456789, z1_10: struct{}{}}, T{a2_0: [2]int16{-1, 2}, u32_1: 4000000000, c64_2: 1 + 2i, i16_3: -1234, up_4: 77, i_5: -42, i8_6: -5, n1_7: N1{3, -7}, u32_8: 4000000000, i_9: -42, z1_10: struct{}{}}}
var aO = [2]O{O{pre: 1, in: T{a2_0: [2]int16{-1, 2}, u32_1: 4000000000, c64_2: 1 + 2i, i16_3: -1234, up_4: 77, i_5: -42, i8_6: -5, n1_7: N1{3, -7}, u32_8: 4000000000, i_9: -42, z1_10: struct{}{}}, post: 2}, O{pre: 3, in: T{a2_0: [2]int16{300, -300}, u32_1: 99, c64_2: -3 - 4i, i16_3: 4321, up_4: 88, i_5: 123456789, i8_6: 7, n1_7: N1{-3, 70000000000}, u32_8: 99, i_9: 123456789, z1_10: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i_5
var pFieldO = &gO.in.i_5
var pElem = &aT[2].i_5
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.a2_0 = [2]int16{300, -300}
		p.u32_1 = 99
		p.c64_2 = -3 - 4i
		p.i16_3 = 4321
		p.up_4 = 88
		p.i_5 = 123456789
		p.i8_6 = 7
		p.n1_7 = N1{-3, 70000000000}
		p.u32_8 = 99
		p.i_9 = 123456789
		p.z1_10 = struct{}{}
	} else {
		p.a2_0 = [2]int16{-1, 2}
		p.u32_1 = 4000000000
		p.c64_2 = 1 + 2i
		p.i16_3 = -1234
		p.up_4 = 77
		p.i_5 = -42
		p.i8_6 = -5
		p.n1_7 = N1{3, -7}
		p.u32_8 = 4000000000
		p.i_9 = -42
		p.z1_10 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("a2_0", unsafe.Offsetof(t.a2_0), unsafe.Sizeof(t.a2_0), unsafe.Alignof(t.a2_0))
	fmt.Println("u32_1", unsafe.Offsetof(t.u32_1), unsafe.Sizeof(t.u32_1), unsafe.Alignof(t.u32_1))
	fmt.Println("c64_2", unsafe.Offsetof(t.c64_2), unsafe.Sizeof(t.c64_2), unsafe.Alignof(t.c64_2))
	fmt.Println("i16_3", unsafe.Offsetof(t.i16_3), unsafe.Sizeof(t.i16_3), unsafe.Alignof(t.i16_3))
	fmt.Println("up_4", unsafe.Offsetof(t.up_4), unsafe.Sizeof(t.up_4), unsafe.Alignof(t.up_4))
	fmt.Println("i_5", unsafe.Offsetof(t.i_5), unsafe.Sizeof(t.i_5), unsafe.Alignof(t.i_5))
	fmt.Println("i8_6", unsafe.Offsetof(t.i8_6), unsafe.Sizeof(t.i8_6), unsafe.Alignof(t.i8_6))
	fmt.Println("n1_7", unsafe.Offsetof(t.n1_7), unsafe.Sizeof(t.n1_7), unsafe.Alignof(t.n1_7))
	fmt.Println("u32_8", unsafe.Offsetof(t.u32_8), unsafe.Sizeof(t.u32_8), unsafe.Alignof(t.u32_8))
	fmt.Println("i_9", unsafe.Offsetof(t.i_9), unsafe.Sizeof(t.i_9), unsafe.Alignof(t.i_9))
	fmt.Println("z1_10", unsafe.Offsetof(t.z1_10), unsafe.Sizeof(t.z1_10), unsafe.Alignof(t.z1_10))
	fmt.Println(unsafe.Offsetof(o.pre), unsafe.Offsetof(o.in), unsafe.Offsetof(o.post))
}

func viaReflect(tag string, v any) {
	rv := reflect.ValueOf(v)
	for i := 0; i < rv.NumField(); i++ {
		f := rv.Field(i)
		if f.Kind() == reflect.Pointer {
			fmt.Println(tag, i, rv.Type().Field(i).Name, f.Elem().Interface(), rv.Type().Field(i).Offset)
		} else {
			fmt.Println(tag, i, rv.Type().Field(i).Name, f.Interface(), rv.Type().Field(i).Offset)
		}
	}
}

func viaUnsafe(p *T) {
	fmt.Println(*(*uint32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u32_1))))
	fmt.Println(*(*int16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i16_3))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_4))))
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_5))))
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_6))))
	fmt.Println(*(*uint32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u32_8))))
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_9))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.a2_0, gT.u32_1, gT.c64_2, gT.i16_3, gT.up_4, gT.i_5, gT.i8_6, gT.n1_7, gT.u32_8, gT.i_9, gT.z1_10)
	fmt.Println("directO", gO.pre, gO.in.a2_0, gO.in.u32_1, gO.in.c64_2, gO.in.i16_3, gO.in.up_4, gO.in.i_5, gO.in.i8_6, gO.in.n1_7, gO.in.u32_8, gO.in.i_9, gO.in.z1_10, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{a2_0: [2]int16{-1, 2}, u32_1: 4000000000, c64_2: 1 + 2i, i16_3: -1234, up_4: 77, i_5: -42, i8_6: -5, n1_7: N1{3, -7}, u32_8: 4000000000, i_9: -42, z1_10: struct{}{}}
	showT("whole store", dT)
	dO = gO
	dO.post = 5
	showO("copy O", dO)
	showO("orig O", gO)
	for i := range aT {
		showT(fmt.Sprint("arr ", i), aT[i])
	}
	for i, e := range sT {
		showTP(fmt.Sprint("slice ", i), &sT[i])
		_ = e
	}
	for i := range aO {
		showOP(fmt.Sprint("arrO ", i), &aO[i])
	}
	mT[1] = gT
	mT[2] = sT[0]
	for k := 1; k <= 2; k++ {
		showT(fmt.Sprint("map ", k), mT[k])
	}
	loc := T{a2_0: [2]int16{300, -300}, u32_1: 99, c64_2: -3 - 4i, i16_3: 4321, up_4: 88, i_5: 123456789, i8_6: 7, n1_7: N1{-3, 70000000000}, u32_8: 99, i_9: 123456789, z1_10: struct{}{}}
	showT("local", loc)
	showTP("local ptr", &loc)
	hp := new(T)
	setDyn(hp, false)
	showTP("heap", hp)
	hp2 := &T{}
	*hp2 = gT
	showTP("heap copy", hp2)
	ho := &O{pre: 4}
	ho.in = gT
	showOP("heap O", ho)
	var any1 any = gT
	showT("iface", any1.(T))
	var any2 any = &gT
	showTP("iface ptr", any2.(*T))
	viaReflect("reflect", gT)
	viaReflect("reflect loc", loc)
	viaReflect("reflect ptr", *any2.(*T))
	viaUnsafe(&gT)
	viaUnsafe(&loc)
	viaUnsafe(hp)
	cp := gT
	fmt.Println(cp == gT, gT == T{a2_0: [2]int16{-1, 2}, u32_1: 4000000000, c64_2: 1 + 2i, i16_3: -1234, up_4: 77, i_5: -42, i8_6: -5, n1_7: N1{3, -7}, u32_8: 4000000000, i_9: -42, z1_10: struct{}{}}, dT == gT)
	cp.z1_10 = struct{}{}
	fmt.Println(cp == gT)
	fmt.Println(aT[0] == aT[2], gO == dO)
	ch := make(chan T, 2)
	ch <- gT
	ch <- loc
	showT("chan 1", <-ch)
	showT("chan 2", <-ch)
	var wg sync.WaitGroup
	res := make([]string, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(v T, o O) {
			defer wg.Done()
			res[i] = fmt.Sprint(v.a2_0, v.u32_1, v.c64_2, v.i16_3, v.up_4, v.i_5, v.i8_6, v.n1_7, v.u32_8, v.i_9, v.z1_10, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
