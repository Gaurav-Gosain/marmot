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
	up_1  uintptr
	i8_2  int8
	i8_3  int8
	r_4   rune
	i16_5 int16
	r_6   rune
	any_7 any
	up_8  uintptr
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.z1_0, x.up_1, x.i8_2, x.i8_3, x.r_4, x.i16_5, x.r_6, x.any_7, x.up_8)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.z1_0, x.up_1, x.i8_2, x.i8_3, x.r_4, x.i16_5, x.r_6, x.any_7, x.up_8)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.z1_0, x.in.up_1, x.in.i8_2, x.in.i8_3, x.in.r_4, x.in.i16_5, x.in.r_6, x.in.any_7, x.in.up_8, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.z1_0, x.in.up_1, x.in.i8_2, x.in.i8_3, x.in.r_4, x.in.i16_5, x.in.r_6, x.in.any_7, x.in.up_8, x.post)
}

var gT = T{z1_0: struct{}{}, up_1: 77, i8_2: -5, i8_3: -5, r_4: 'x', i16_5: -1234, r_6: 'x', any_7: 42, up_8: 77}
var gO = O{pre: 7, in: T{z1_0: struct{}{}, up_1: 77, i8_2: -5, i8_3: -5, r_4: 'x', i16_5: -1234, r_6: 'x', any_7: 42, up_8: 77}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{z1_0: struct{}{}, up_1: 77, i8_2: -5, i8_3: -5, r_4: 'x', i16_5: -1234, r_6: 'x', any_7: 42, up_8: 77}, {}, T{z1_0: struct{}{}, up_1: 88, i8_2: 7, i8_3: 7, r_4: '世', i16_5: 4321, r_6: '世', any_7: "dyn", up_8: 88}}
var sT = []T{T{z1_0: struct{}{}, up_1: 88, i8_2: 7, i8_3: 7, r_4: '世', i16_5: 4321, r_6: '世', any_7: "dyn", up_8: 88}, T{z1_0: struct{}{}, up_1: 77, i8_2: -5, i8_3: -5, r_4: 'x', i16_5: -1234, r_6: 'x', any_7: 42, up_8: 77}}
var aO = [2]O{O{pre: 1, in: T{z1_0: struct{}{}, up_1: 77, i8_2: -5, i8_3: -5, r_4: 'x', i16_5: -1234, r_6: 'x', any_7: 42, up_8: 77}, post: 2}, O{pre: 3, in: T{z1_0: struct{}{}, up_1: 88, i8_2: 7, i8_3: 7, r_4: '世', i16_5: 4321, r_6: '世', any_7: "dyn", up_8: 88}, post: 4}}
var mT = map[int]T{}
var pField = &gT.z1_0
var pFieldO = &gO.in.z1_0
var pElem = &aT[2].z1_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.z1_0 = struct{}{}
		p.up_1 = 88
		p.i8_2 = 7
		p.i8_3 = 7
		p.r_4 = '世'
		p.i16_5 = 4321
		p.r_6 = '世'
		p.any_7 = "dyn"
		p.up_8 = 88
	} else {
		p.z1_0 = struct{}{}
		p.up_1 = 77
		p.i8_2 = -5
		p.i8_3 = -5
		p.r_4 = 'x'
		p.i16_5 = -1234
		p.r_6 = 'x'
		p.any_7 = 42
		p.up_8 = 77
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("z1_0", unsafe.Offsetof(t.z1_0), unsafe.Sizeof(t.z1_0), unsafe.Alignof(t.z1_0))
	fmt.Println("up_1", unsafe.Offsetof(t.up_1), unsafe.Sizeof(t.up_1), unsafe.Alignof(t.up_1))
	fmt.Println("i8_2", unsafe.Offsetof(t.i8_2), unsafe.Sizeof(t.i8_2), unsafe.Alignof(t.i8_2))
	fmt.Println("i8_3", unsafe.Offsetof(t.i8_3), unsafe.Sizeof(t.i8_3), unsafe.Alignof(t.i8_3))
	fmt.Println("r_4", unsafe.Offsetof(t.r_4), unsafe.Sizeof(t.r_4), unsafe.Alignof(t.r_4))
	fmt.Println("i16_5", unsafe.Offsetof(t.i16_5), unsafe.Sizeof(t.i16_5), unsafe.Alignof(t.i16_5))
	fmt.Println("r_6", unsafe.Offsetof(t.r_6), unsafe.Sizeof(t.r_6), unsafe.Alignof(t.r_6))
	fmt.Println("any_7", unsafe.Offsetof(t.any_7), unsafe.Sizeof(t.any_7), unsafe.Alignof(t.any_7))
	fmt.Println("up_8", unsafe.Offsetof(t.up_8), unsafe.Sizeof(t.up_8), unsafe.Alignof(t.up_8))
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
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_1))))
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_2))))
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_3))))
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_4))))
	fmt.Println(*(*int16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i16_5))))
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_6))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_8))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.z1_0, gT.up_1, gT.i8_2, gT.i8_3, gT.r_4, gT.i16_5, gT.r_6, gT.any_7, gT.up_8)
	fmt.Println("directO", gO.pre, gO.in.z1_0, gO.in.up_1, gO.in.i8_2, gO.in.i8_3, gO.in.r_4, gO.in.i16_5, gO.in.r_6, gO.in.any_7, gO.in.up_8, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{z1_0: struct{}{}, up_1: 77, i8_2: -5, i8_3: -5, r_4: 'x', i16_5: -1234, r_6: 'x', any_7: 42, up_8: 77}
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
	loc := T{z1_0: struct{}{}, up_1: 88, i8_2: 7, i8_3: 7, r_4: '世', i16_5: 4321, r_6: '世', any_7: "dyn", up_8: 88}
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
	fmt.Println(cp == gT, gT == T{z1_0: struct{}{}, up_1: 77, i8_2: -5, i8_3: -5, r_4: 'x', i16_5: -1234, r_6: 'x', any_7: 42, up_8: 77}, dT == gT)
	cp.up_8 = 88
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
			res[i] = fmt.Sprint(v.z1_0, v.up_1, v.i8_2, v.i8_3, v.r_4, v.i16_5, v.r_6, v.any_7, v.up_8, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
