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
	i_0   int
	i8_1  int8
	n2_2  N2
	u64_3 uint64
	f32_4 float32
	f64_5 float64
	c64_6 complex64
	up_7  uintptr
	f64_8 float64
	u32_9 uint32
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
	fmt.Println(tag, x.i_0, x.i8_1, x.n2_2, x.u64_3, x.f32_4, x.f64_5, x.c64_6, x.up_7, x.f64_8, x.u32_9, x.z1_10)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.i_0, x.i8_1, x.n2_2, x.u64_3, x.f32_4, x.f64_5, x.c64_6, x.up_7, x.f64_8, x.u32_9, x.z1_10)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.i_0, x.in.i8_1, x.in.n2_2, x.in.u64_3, x.in.f32_4, x.in.f64_5, x.in.c64_6, x.in.up_7, x.in.f64_8, x.in.u32_9, x.in.z1_10, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.i_0, x.in.i8_1, x.in.n2_2, x.in.u64_3, x.in.f32_4, x.in.f64_5, x.in.c64_6, x.in.up_7, x.in.f64_8, x.in.u32_9, x.in.z1_10, x.post)
}

var gT = T{i_0: -42, i8_1: -5, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_3: 18000000000000000000, f32_4: 1.5, f64_5: -2.25, c64_6: 1 + 2i, up_7: 77, f64_8: -2.25, u32_9: 4000000000, z1_10: struct{}{}}
var gO = O{pre: 7, in: T{i_0: -42, i8_1: -5, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_3: 18000000000000000000, f32_4: 1.5, f64_5: -2.25, c64_6: 1 + 2i, up_7: 77, f64_8: -2.25, u32_9: 4000000000, z1_10: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{i_0: -42, i8_1: -5, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_3: 18000000000000000000, f32_4: 1.5, f64_5: -2.25, c64_6: 1 + 2i, up_7: 77, f64_8: -2.25, u32_9: 4000000000, z1_10: struct{}{}}, {}, T{i_0: 123456789, i8_1: 7, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, u64_3: 77, f32_4: -2.75, f64_5: 1e100, c64_6: -3 - 4i, up_7: 88, f64_8: 1e100, u32_9: 99, z1_10: struct{}{}}}
var sT = []T{T{i_0: 123456789, i8_1: 7, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, u64_3: 77, f32_4: -2.75, f64_5: 1e100, c64_6: -3 - 4i, up_7: 88, f64_8: 1e100, u32_9: 99, z1_10: struct{}{}}, T{i_0: -42, i8_1: -5, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_3: 18000000000000000000, f32_4: 1.5, f64_5: -2.25, c64_6: 1 + 2i, up_7: 77, f64_8: -2.25, u32_9: 4000000000, z1_10: struct{}{}}}
var aO = [2]O{O{pre: 1, in: T{i_0: -42, i8_1: -5, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_3: 18000000000000000000, f32_4: 1.5, f64_5: -2.25, c64_6: 1 + 2i, up_7: 77, f64_8: -2.25, u32_9: 4000000000, z1_10: struct{}{}}, post: 2}, O{pre: 3, in: T{i_0: 123456789, i8_1: 7, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, u64_3: 77, f32_4: -2.75, f64_5: 1e100, c64_6: -3 - 4i, up_7: 88, f64_8: 1e100, u32_9: 99, z1_10: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i_0
var pFieldO = &gO.in.i_0
var pElem = &aT[2].i_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.i_0 = 123456789
		p.i8_1 = 7
		p.n2_2 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.u64_3 = 77
		p.f32_4 = -2.75
		p.f64_5 = 1e100
		p.c64_6 = -3 - 4i
		p.up_7 = 88
		p.f64_8 = 1e100
		p.u32_9 = 99
		p.z1_10 = struct{}{}
	} else {
		p.i_0 = -42
		p.i8_1 = -5
		p.n2_2 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.u64_3 = 18000000000000000000
		p.f32_4 = 1.5
		p.f64_5 = -2.25
		p.c64_6 = 1 + 2i
		p.up_7 = 77
		p.f64_8 = -2.25
		p.u32_9 = 4000000000
		p.z1_10 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("i_0", unsafe.Offsetof(t.i_0), unsafe.Sizeof(t.i_0), unsafe.Alignof(t.i_0))
	fmt.Println("i8_1", unsafe.Offsetof(t.i8_1), unsafe.Sizeof(t.i8_1), unsafe.Alignof(t.i8_1))
	fmt.Println("n2_2", unsafe.Offsetof(t.n2_2), unsafe.Sizeof(t.n2_2), unsafe.Alignof(t.n2_2))
	fmt.Println("u64_3", unsafe.Offsetof(t.u64_3), unsafe.Sizeof(t.u64_3), unsafe.Alignof(t.u64_3))
	fmt.Println("f32_4", unsafe.Offsetof(t.f32_4), unsafe.Sizeof(t.f32_4), unsafe.Alignof(t.f32_4))
	fmt.Println("f64_5", unsafe.Offsetof(t.f64_5), unsafe.Sizeof(t.f64_5), unsafe.Alignof(t.f64_5))
	fmt.Println("c64_6", unsafe.Offsetof(t.c64_6), unsafe.Sizeof(t.c64_6), unsafe.Alignof(t.c64_6))
	fmt.Println("up_7", unsafe.Offsetof(t.up_7), unsafe.Sizeof(t.up_7), unsafe.Alignof(t.up_7))
	fmt.Println("f64_8", unsafe.Offsetof(t.f64_8), unsafe.Sizeof(t.f64_8), unsafe.Alignof(t.f64_8))
	fmt.Println("u32_9", unsafe.Offsetof(t.u32_9), unsafe.Sizeof(t.u32_9), unsafe.Alignof(t.u32_9))
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
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_0))))
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_1))))
	fmt.Println(*(*uint64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u64_3))))
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_4))))
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_5))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_7))))
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_8))))
	fmt.Println(*(*uint32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u32_9))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.i_0, gT.i8_1, gT.n2_2, gT.u64_3, gT.f32_4, gT.f64_5, gT.c64_6, gT.up_7, gT.f64_8, gT.u32_9, gT.z1_10)
	fmt.Println("directO", gO.pre, gO.in.i_0, gO.in.i8_1, gO.in.n2_2, gO.in.u64_3, gO.in.f32_4, gO.in.f64_5, gO.in.c64_6, gO.in.up_7, gO.in.f64_8, gO.in.u32_9, gO.in.z1_10, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{i_0: -42, i8_1: -5, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_3: 18000000000000000000, f32_4: 1.5, f64_5: -2.25, c64_6: 1 + 2i, up_7: 77, f64_8: -2.25, u32_9: 4000000000, z1_10: struct{}{}}
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
	loc := T{i_0: 123456789, i8_1: 7, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, u64_3: 77, f32_4: -2.75, f64_5: 1e100, c64_6: -3 - 4i, up_7: 88, f64_8: 1e100, u32_9: 99, z1_10: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{i_0: -42, i8_1: -5, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_3: 18000000000000000000, f32_4: 1.5, f64_5: -2.25, c64_6: 1 + 2i, up_7: 77, f64_8: -2.25, u32_9: 4000000000, z1_10: struct{}{}}, dT == gT)
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
			res[i] = fmt.Sprint(v.i_0, v.i8_1, v.n2_2, v.u64_3, v.f32_4, v.f64_5, v.c64_6, v.up_7, v.f64_8, v.u32_9, v.z1_10, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
