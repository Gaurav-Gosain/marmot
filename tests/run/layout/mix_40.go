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
	n2_0  N2
	f64_1 float64
	sl_2  []int
	i8_3  int8
	i_4   int
	u16_5 uint16
	u32_6 uint32
	s_7   string
	n2_8  N2
	z1_9  struct{}
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.n2_0, x.f64_1, x.sl_2, x.i8_3, x.i_4, x.u16_5, x.u32_6, x.s_7, x.n2_8, x.z1_9)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.n2_0, x.f64_1, x.sl_2, x.i8_3, x.i_4, x.u16_5, x.u32_6, x.s_7, x.n2_8, x.z1_9)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.n2_0, x.f64_1, x.sl_2, x.i8_3, x.i_4, x.u16_5, x.u32_6, x.s_7, x.n2_8, x.z1_9, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.n2_0, x.f64_1, x.sl_2, x.i8_3, x.i_4, x.u16_5, x.u32_6, x.s_7, x.n2_8, x.z1_9, x.post)
}

var gT = T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, f64_1: -2.25, sl_2: []int{1, 2, 3}, i8_3: -5, i_4: -42, u16_5: 60000, u32_6: 4000000000, s_7: "héllo\t", n2_8: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_9: struct{}{}}
var gO = O{pre: 7, T: T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, f64_1: -2.25, sl_2: []int{1, 2, 3}, i8_3: -5, i_4: -42, u16_5: 60000, u32_6: 4000000000, s_7: "héllo\t", n2_8: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_9: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, f64_1: -2.25, sl_2: []int{1, 2, 3}, i8_3: -5, i_4: -42, u16_5: 60000, u32_6: 4000000000, s_7: "héllo\t", n2_8: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_9: struct{}{}}, {}, T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, f64_1: 1e100, sl_2: []int{9}, i8_3: 7, i_4: 123456789, u16_5: 513, u32_6: 99, s_7: "x", n2_8: N2{false, [3]byte{1, 1, 1}, -0.5}, z1_9: struct{}{}}}
var sT = []T{T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, f64_1: 1e100, sl_2: []int{9}, i8_3: 7, i_4: 123456789, u16_5: 513, u32_6: 99, s_7: "x", n2_8: N2{false, [3]byte{1, 1, 1}, -0.5}, z1_9: struct{}{}}, T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, f64_1: -2.25, sl_2: []int{1, 2, 3}, i8_3: -5, i_4: -42, u16_5: 60000, u32_6: 4000000000, s_7: "héllo\t", n2_8: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_9: struct{}{}}}
var aO = [2]O{O{pre: 1, T: T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, f64_1: -2.25, sl_2: []int{1, 2, 3}, i8_3: -5, i_4: -42, u16_5: 60000, u32_6: 4000000000, s_7: "héllo\t", n2_8: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_9: struct{}{}}, post: 2}, O{pre: 3, T: T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, f64_1: 1e100, sl_2: []int{9}, i8_3: 7, i_4: 123456789, u16_5: 513, u32_6: 99, s_7: "x", n2_8: N2{false, [3]byte{1, 1, 1}, -0.5}, z1_9: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.f64_1
var pFieldO = &gO.f64_1
var pElem = &aT[2].f64_1
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.n2_0 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.f64_1 = 1e100
		p.sl_2 = []int{9}
		p.i8_3 = 7
		p.i_4 = 123456789
		p.u16_5 = 513
		p.u32_6 = 99
		p.s_7 = "x"
		p.n2_8 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.z1_9 = struct{}{}
	} else {
		p.n2_0 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.f64_1 = -2.25
		p.sl_2 = []int{1, 2, 3}
		p.i8_3 = -5
		p.i_4 = -42
		p.u16_5 = 60000
		p.u32_6 = 4000000000
		p.s_7 = "héllo\t"
		p.n2_8 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.z1_9 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("n2_0", unsafe.Offsetof(t.n2_0), unsafe.Sizeof(t.n2_0), unsafe.Alignof(t.n2_0))
	fmt.Println("f64_1", unsafe.Offsetof(t.f64_1), unsafe.Sizeof(t.f64_1), unsafe.Alignof(t.f64_1))
	fmt.Println("sl_2", unsafe.Offsetof(t.sl_2), unsafe.Sizeof(t.sl_2), unsafe.Alignof(t.sl_2))
	fmt.Println("i8_3", unsafe.Offsetof(t.i8_3), unsafe.Sizeof(t.i8_3), unsafe.Alignof(t.i8_3))
	fmt.Println("i_4", unsafe.Offsetof(t.i_4), unsafe.Sizeof(t.i_4), unsafe.Alignof(t.i_4))
	fmt.Println("u16_5", unsafe.Offsetof(t.u16_5), unsafe.Sizeof(t.u16_5), unsafe.Alignof(t.u16_5))
	fmt.Println("u32_6", unsafe.Offsetof(t.u32_6), unsafe.Sizeof(t.u32_6), unsafe.Alignof(t.u32_6))
	fmt.Println("s_7", unsafe.Offsetof(t.s_7), unsafe.Sizeof(t.s_7), unsafe.Alignof(t.s_7))
	fmt.Println("n2_8", unsafe.Offsetof(t.n2_8), unsafe.Sizeof(t.n2_8), unsafe.Alignof(t.n2_8))
	fmt.Println("z1_9", unsafe.Offsetof(t.z1_9), unsafe.Sizeof(t.z1_9), unsafe.Alignof(t.z1_9))
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
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_1))))
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_3))))
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_4))))
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_5))))
	fmt.Println(*(*uint32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u32_6))))
	fmt.Println(*(*string)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.s_7))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.n2_0, gT.f64_1, gT.sl_2, gT.i8_3, gT.i_4, gT.u16_5, gT.u32_6, gT.s_7, gT.n2_8, gT.z1_9)
	fmt.Println("directO", gO.pre, gO.n2_0, gO.f64_1, gO.sl_2, gO.i8_3, gO.i_4, gO.u16_5, gO.u32_6, gO.s_7, gO.n2_8, gO.z1_9, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, f64_1: -2.25, sl_2: []int{1, 2, 3}, i8_3: -5, i_4: -42, u16_5: 60000, u32_6: 4000000000, s_7: "héllo\t", n2_8: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_9: struct{}{}}
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
	loc := T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, f64_1: 1e100, sl_2: []int{9}, i8_3: 7, i_4: 123456789, u16_5: 513, u32_6: 99, s_7: "x", n2_8: N2{false, [3]byte{1, 1, 1}, -0.5}, z1_9: struct{}{}}
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
			res[i] = fmt.Sprint(v.n2_0, v.f64_1, v.sl_2, v.i8_3, v.i_4, v.u16_5, v.u32_6, v.s_7, v.n2_8, v.z1_9, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
