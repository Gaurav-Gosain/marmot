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
	z1_0  struct{}
	i_1   int
	b_2   bool
	any_3 any
	c64_4 complex64
	i_5   int
	up_6  uintptr
	i8_7  int8
	i16_8 int16
	c64_9 complex64
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.z1_0, x.i_1, x.b_2, x.any_3, x.c64_4, x.i_5, x.up_6, x.i8_7, x.i16_8, x.c64_9)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.z1_0, x.i_1, x.b_2, x.any_3, x.c64_4, x.i_5, x.up_6, x.i8_7, x.i16_8, x.c64_9)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.z1_0, x.i_1, x.b_2, x.any_3, x.c64_4, x.i_5, x.up_6, x.i8_7, x.i16_8, x.c64_9, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.z1_0, x.i_1, x.b_2, x.any_3, x.c64_4, x.i_5, x.up_6, x.i8_7, x.i16_8, x.c64_9, x.post)
}

var gT = T{z1_0: struct{}{}, i_1: -42, b_2: true, any_3: 42, c64_4: 1 + 2i, i_5: -42, up_6: 77, i8_7: -5, i16_8: -1234, c64_9: 1 + 2i}
var gO = O{pre: 7, T: T{z1_0: struct{}{}, i_1: -42, b_2: true, any_3: 42, c64_4: 1 + 2i, i_5: -42, up_6: 77, i8_7: -5, i16_8: -1234, c64_9: 1 + 2i}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{z1_0: struct{}{}, i_1: -42, b_2: true, any_3: 42, c64_4: 1 + 2i, i_5: -42, up_6: 77, i8_7: -5, i16_8: -1234, c64_9: 1 + 2i}, {}, T{z1_0: struct{}{}, i_1: 123456789, b_2: false, any_3: "dyn", c64_4: -3 - 4i, i_5: 123456789, up_6: 88, i8_7: 7, i16_8: 4321, c64_9: -3 - 4i}}
var sT = []T{T{z1_0: struct{}{}, i_1: 123456789, b_2: false, any_3: "dyn", c64_4: -3 - 4i, i_5: 123456789, up_6: 88, i8_7: 7, i16_8: 4321, c64_9: -3 - 4i}, T{z1_0: struct{}{}, i_1: -42, b_2: true, any_3: 42, c64_4: 1 + 2i, i_5: -42, up_6: 77, i8_7: -5, i16_8: -1234, c64_9: 1 + 2i}}
var aO = [2]O{O{pre: 1, T: T{z1_0: struct{}{}, i_1: -42, b_2: true, any_3: 42, c64_4: 1 + 2i, i_5: -42, up_6: 77, i8_7: -5, i16_8: -1234, c64_9: 1 + 2i}, post: 2}, O{pre: 3, T: T{z1_0: struct{}{}, i_1: 123456789, b_2: false, any_3: "dyn", c64_4: -3 - 4i, i_5: 123456789, up_6: 88, i8_7: 7, i16_8: 4321, c64_9: -3 - 4i}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i_1
var pFieldO = &gO.i_1
var pElem = &aT[2].i_1
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.z1_0 = struct{}{}
		p.i_1 = 123456789
		p.b_2 = false
		p.any_3 = "dyn"
		p.c64_4 = -3 - 4i
		p.i_5 = 123456789
		p.up_6 = 88
		p.i8_7 = 7
		p.i16_8 = 4321
		p.c64_9 = -3 - 4i
	} else {
		p.z1_0 = struct{}{}
		p.i_1 = -42
		p.b_2 = true
		p.any_3 = 42
		p.c64_4 = 1 + 2i
		p.i_5 = -42
		p.up_6 = 77
		p.i8_7 = -5
		p.i16_8 = -1234
		p.c64_9 = 1 + 2i
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("z1_0", unsafe.Offsetof(t.z1_0), unsafe.Sizeof(t.z1_0), unsafe.Alignof(t.z1_0))
	fmt.Println("i_1", unsafe.Offsetof(t.i_1), unsafe.Sizeof(t.i_1), unsafe.Alignof(t.i_1))
	fmt.Println("b_2", unsafe.Offsetof(t.b_2), unsafe.Sizeof(t.b_2), unsafe.Alignof(t.b_2))
	fmt.Println("any_3", unsafe.Offsetof(t.any_3), unsafe.Sizeof(t.any_3), unsafe.Alignof(t.any_3))
	fmt.Println("c64_4", unsafe.Offsetof(t.c64_4), unsafe.Sizeof(t.c64_4), unsafe.Alignof(t.c64_4))
	fmt.Println("i_5", unsafe.Offsetof(t.i_5), unsafe.Sizeof(t.i_5), unsafe.Alignof(t.i_5))
	fmt.Println("up_6", unsafe.Offsetof(t.up_6), unsafe.Sizeof(t.up_6), unsafe.Alignof(t.up_6))
	fmt.Println("i8_7", unsafe.Offsetof(t.i8_7), unsafe.Sizeof(t.i8_7), unsafe.Alignof(t.i8_7))
	fmt.Println("i16_8", unsafe.Offsetof(t.i16_8), unsafe.Sizeof(t.i16_8), unsafe.Alignof(t.i16_8))
	fmt.Println("c64_9", unsafe.Offsetof(t.c64_9), unsafe.Sizeof(t.c64_9), unsafe.Alignof(t.c64_9))
	fmt.Println(unsafe.Offsetof(o.pre), unsafe.Offsetof(o.T), unsafe.Offsetof(o.post))
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
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_1))))
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_2))))
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_5))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_6))))
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_7))))
	fmt.Println(*(*int16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i16_8))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.z1_0, gT.i_1, gT.b_2, gT.any_3, gT.c64_4, gT.i_5, gT.up_6, gT.i8_7, gT.i16_8, gT.c64_9)
	fmt.Println("directO", gO.pre, gO.z1_0, gO.i_1, gO.b_2, gO.any_3, gO.c64_4, gO.i_5, gO.up_6, gO.i8_7, gO.i16_8, gO.c64_9, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{z1_0: struct{}{}, i_1: -42, b_2: true, any_3: 42, c64_4: 1 + 2i, i_5: -42, up_6: 77, i8_7: -5, i16_8: -1234, c64_9: 1 + 2i}
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
	loc := T{z1_0: struct{}{}, i_1: 123456789, b_2: false, any_3: "dyn", c64_4: -3 - 4i, i_5: 123456789, up_6: 88, i8_7: 7, i16_8: 4321, c64_9: -3 - 4i}
	showT("local", loc)
	showTP("local ptr", &loc)
	hp := new(T)
	setDyn(hp, false)
	showTP("heap", hp)
	hp2 := &T{}
	*hp2 = gT
	showTP("heap copy", hp2)
	ho := &O{pre: 4}
	ho.T = gT
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
	fmt.Println(cp == gT, gT == T{z1_0: struct{}{}, i_1: -42, b_2: true, any_3: 42, c64_4: 1 + 2i, i_5: -42, up_6: 77, i8_7: -5, i16_8: -1234, c64_9: 1 + 2i}, dT == gT)
	cp.c64_9 = -3 - 4i
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
			res[i] = fmt.Sprint(v.z1_0, v.i_1, v.b_2, v.any_3, v.c64_4, v.i_5, v.up_6, v.i8_7, v.i16_8, v.c64_9, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
