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
	any_0  any
	u16_1  uint16
	u8_2   uint8
	i32_3  int32
	c128_4 complex128
	i64_5  int64
	i64_6  int64
	z1_7   struct{}
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.any_0, x.u16_1, x.u8_2, x.i32_3, x.c128_4, x.i64_5, x.i64_6, x.z1_7)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.any_0, x.u16_1, x.u8_2, x.i32_3, x.c128_4, x.i64_5, x.i64_6, x.z1_7)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.any_0, x.u16_1, x.u8_2, x.i32_3, x.c128_4, x.i64_5, x.i64_6, x.z1_7, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.any_0, x.u16_1, x.u8_2, x.i32_3, x.c128_4, x.i64_5, x.i64_6, x.z1_7, x.post)
}

var gT = T{any_0: 42, u16_1: 60000, u8_2: 200, i32_3: -100000, c128_4: 3.5 - 1i, i64_5: -9000000000, i64_6: -9000000000, z1_7: struct{}{}}
var gO = O{pre: 7, T: T{any_0: 42, u16_1: 60000, u8_2: 200, i32_3: -100000, c128_4: 3.5 - 1i, i64_5: -9000000000, i64_6: -9000000000, z1_7: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{any_0: 42, u16_1: 60000, u8_2: 200, i32_3: -100000, c128_4: 3.5 - 1i, i64_5: -9000000000, i64_6: -9000000000, z1_7: struct{}{}}, {}, T{any_0: "dyn", u16_1: 513, u8_2: 13, i32_3: 70000, c128_4: 0.5 + 0.25i, i64_5: 8000000000, i64_6: 8000000000, z1_7: struct{}{}}}
var sT = []T{T{any_0: "dyn", u16_1: 513, u8_2: 13, i32_3: 70000, c128_4: 0.5 + 0.25i, i64_5: 8000000000, i64_6: 8000000000, z1_7: struct{}{}}, T{any_0: 42, u16_1: 60000, u8_2: 200, i32_3: -100000, c128_4: 3.5 - 1i, i64_5: -9000000000, i64_6: -9000000000, z1_7: struct{}{}}}
var aO = [2]O{O{pre: 1, T: T{any_0: 42, u16_1: 60000, u8_2: 200, i32_3: -100000, c128_4: 3.5 - 1i, i64_5: -9000000000, i64_6: -9000000000, z1_7: struct{}{}}, post: 2}, O{pre: 3, T: T{any_0: "dyn", u16_1: 513, u8_2: 13, i32_3: 70000, c128_4: 0.5 + 0.25i, i64_5: 8000000000, i64_6: 8000000000, z1_7: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.u8_2
var pFieldO = &gO.u8_2
var pElem = &aT[2].u8_2
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.any_0 = "dyn"
		p.u16_1 = 513
		p.u8_2 = 13
		p.i32_3 = 70000
		p.c128_4 = 0.5 + 0.25i
		p.i64_5 = 8000000000
		p.i64_6 = 8000000000
		p.z1_7 = struct{}{}
	} else {
		p.any_0 = 42
		p.u16_1 = 60000
		p.u8_2 = 200
		p.i32_3 = -100000
		p.c128_4 = 3.5 - 1i
		p.i64_5 = -9000000000
		p.i64_6 = -9000000000
		p.z1_7 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("any_0", unsafe.Offsetof(t.any_0), unsafe.Sizeof(t.any_0), unsafe.Alignof(t.any_0))
	fmt.Println("u16_1", unsafe.Offsetof(t.u16_1), unsafe.Sizeof(t.u16_1), unsafe.Alignof(t.u16_1))
	fmt.Println("u8_2", unsafe.Offsetof(t.u8_2), unsafe.Sizeof(t.u8_2), unsafe.Alignof(t.u8_2))
	fmt.Println("i32_3", unsafe.Offsetof(t.i32_3), unsafe.Sizeof(t.i32_3), unsafe.Alignof(t.i32_3))
	fmt.Println("c128_4", unsafe.Offsetof(t.c128_4), unsafe.Sizeof(t.c128_4), unsafe.Alignof(t.c128_4))
	fmt.Println("i64_5", unsafe.Offsetof(t.i64_5), unsafe.Sizeof(t.i64_5), unsafe.Alignof(t.i64_5))
	fmt.Println("i64_6", unsafe.Offsetof(t.i64_6), unsafe.Sizeof(t.i64_6), unsafe.Alignof(t.i64_6))
	fmt.Println("z1_7", unsafe.Offsetof(t.z1_7), unsafe.Sizeof(t.z1_7), unsafe.Alignof(t.z1_7))
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
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_1))))
	fmt.Println(*(*uint8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u8_2))))
	fmt.Println(*(*int32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i32_3))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_5))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_6))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.any_0, gT.u16_1, gT.u8_2, gT.i32_3, gT.c128_4, gT.i64_5, gT.i64_6, gT.z1_7)
	fmt.Println("directO", gO.pre, gO.any_0, gO.u16_1, gO.u8_2, gO.i32_3, gO.c128_4, gO.i64_5, gO.i64_6, gO.z1_7, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{any_0: 42, u16_1: 60000, u8_2: 200, i32_3: -100000, c128_4: 3.5 - 1i, i64_5: -9000000000, i64_6: -9000000000, z1_7: struct{}{}}
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
	loc := T{any_0: "dyn", u16_1: 513, u8_2: 13, i32_3: 70000, c128_4: 0.5 + 0.25i, i64_5: 8000000000, i64_6: 8000000000, z1_7: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{any_0: 42, u16_1: 60000, u8_2: 200, i32_3: -100000, c128_4: 3.5 - 1i, i64_5: -9000000000, i64_6: -9000000000, z1_7: struct{}{}}, dT == gT)
	cp.z1_7 = struct{}{}
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
			res[i] = fmt.Sprint(v.any_0, v.u16_1, v.u8_2, v.i32_3, v.c128_4, v.i64_5, v.i64_6, v.z1_7, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
