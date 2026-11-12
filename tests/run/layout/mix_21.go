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
	z2_0  [0]int64
	u8_1  uint8
	n1_2  N1
	n1_3  N1
	c64_4 complex64
	i8_5  int8
	n2_6  N2
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.z2_0, x.u8_1, x.n1_2, x.n1_3, x.c64_4, x.i8_5, x.n2_6)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.z2_0, x.u8_1, x.n1_2, x.n1_3, x.c64_4, x.i8_5, x.n2_6)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.z2_0, x.in.u8_1, x.in.n1_2, x.in.n1_3, x.in.c64_4, x.in.i8_5, x.in.n2_6, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.z2_0, x.in.u8_1, x.in.n1_2, x.in.n1_3, x.in.c64_4, x.in.i8_5, x.in.n2_6, x.post)
}

var gT = T{z2_0: [0]int64{}, u8_1: 200, n1_2: N1{3, -7}, n1_3: N1{3, -7}, c64_4: 1 + 2i, i8_5: -5, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}}
var gO = O{pre: 7, in: T{z2_0: [0]int64{}, u8_1: 200, n1_2: N1{3, -7}, n1_3: N1{3, -7}, c64_4: 1 + 2i, i8_5: -5, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{z2_0: [0]int64{}, u8_1: 200, n1_2: N1{3, -7}, n1_3: N1{3, -7}, c64_4: 1 + 2i, i8_5: -5, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}}, {}, T{z2_0: [0]int64{}, u8_1: 13, n1_2: N1{-3, 70000000000}, n1_3: N1{-3, 70000000000}, c64_4: -3 - 4i, i8_5: 7, n2_6: N2{false, [3]byte{1, 1, 1}, -0.5}}}
var sT = []T{T{z2_0: [0]int64{}, u8_1: 13, n1_2: N1{-3, 70000000000}, n1_3: N1{-3, 70000000000}, c64_4: -3 - 4i, i8_5: 7, n2_6: N2{false, [3]byte{1, 1, 1}, -0.5}}, T{z2_0: [0]int64{}, u8_1: 200, n1_2: N1{3, -7}, n1_3: N1{3, -7}, c64_4: 1 + 2i, i8_5: -5, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}}}
var aO = [2]O{O{pre: 1, in: T{z2_0: [0]int64{}, u8_1: 200, n1_2: N1{3, -7}, n1_3: N1{3, -7}, c64_4: 1 + 2i, i8_5: -5, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}}, post: 2}, O{pre: 3, in: T{z2_0: [0]int64{}, u8_1: 13, n1_2: N1{-3, 70000000000}, n1_3: N1{-3, 70000000000}, c64_4: -3 - 4i, i8_5: 7, n2_6: N2{false, [3]byte{1, 1, 1}, -0.5}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.u8_1
var pFieldO = &gO.in.u8_1
var pElem = &aT[2].u8_1
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.z2_0 = [0]int64{}
		p.u8_1 = 13
		p.n1_2 = N1{-3, 70000000000}
		p.n1_3 = N1{-3, 70000000000}
		p.c64_4 = -3 - 4i
		p.i8_5 = 7
		p.n2_6 = N2{false, [3]byte{1, 1, 1}, -0.5}
	} else {
		p.z2_0 = [0]int64{}
		p.u8_1 = 200
		p.n1_2 = N1{3, -7}
		p.n1_3 = N1{3, -7}
		p.c64_4 = 1 + 2i
		p.i8_5 = -5
		p.n2_6 = N2{true, [3]byte{9, 8, 7}, 2.5}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("z2_0", unsafe.Offsetof(t.z2_0), unsafe.Sizeof(t.z2_0), unsafe.Alignof(t.z2_0))
	fmt.Println("u8_1", unsafe.Offsetof(t.u8_1), unsafe.Sizeof(t.u8_1), unsafe.Alignof(t.u8_1))
	fmt.Println("n1_2", unsafe.Offsetof(t.n1_2), unsafe.Sizeof(t.n1_2), unsafe.Alignof(t.n1_2))
	fmt.Println("n1_3", unsafe.Offsetof(t.n1_3), unsafe.Sizeof(t.n1_3), unsafe.Alignof(t.n1_3))
	fmt.Println("c64_4", unsafe.Offsetof(t.c64_4), unsafe.Sizeof(t.c64_4), unsafe.Alignof(t.c64_4))
	fmt.Println("i8_5", unsafe.Offsetof(t.i8_5), unsafe.Sizeof(t.i8_5), unsafe.Alignof(t.i8_5))
	fmt.Println("n2_6", unsafe.Offsetof(t.n2_6), unsafe.Sizeof(t.n2_6), unsafe.Alignof(t.n2_6))
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
	fmt.Println(*(*uint8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u8_1))))
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_5))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.z2_0, gT.u8_1, gT.n1_2, gT.n1_3, gT.c64_4, gT.i8_5, gT.n2_6)
	fmt.Println("directO", gO.pre, gO.in.z2_0, gO.in.u8_1, gO.in.n1_2, gO.in.n1_3, gO.in.c64_4, gO.in.i8_5, gO.in.n2_6, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{z2_0: [0]int64{}, u8_1: 200, n1_2: N1{3, -7}, n1_3: N1{3, -7}, c64_4: 1 + 2i, i8_5: -5, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}}
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
	loc := T{z2_0: [0]int64{}, u8_1: 13, n1_2: N1{-3, 70000000000}, n1_3: N1{-3, 70000000000}, c64_4: -3 - 4i, i8_5: 7, n2_6: N2{false, [3]byte{1, 1, 1}, -0.5}}
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
	fmt.Println(cp == gT, gT == T{z2_0: [0]int64{}, u8_1: 200, n1_2: N1{3, -7}, n1_3: N1{3, -7}, c64_4: 1 + 2i, i8_5: -5, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}}, dT == gT)
	cp.n2_6 = N2{false, [3]byte{1, 1, 1}, -0.5}
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
			res[i] = fmt.Sprint(v.z2_0, v.u8_1, v.n1_2, v.n1_3, v.c64_4, v.i8_5, v.n2_6, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
