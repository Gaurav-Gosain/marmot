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
	sl_0  []int
	u8_1  uint8
	n2_2  N2
	z1_3  struct{}
	up_4  uintptr
	i64_5 int64
	f64_6 float64
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.sl_0, x.u8_1, x.n2_2, x.z1_3, x.up_4, x.i64_5, x.f64_6)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.sl_0, x.u8_1, x.n2_2, x.z1_3, x.up_4, x.i64_5, x.f64_6)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.sl_0, x.u8_1, x.n2_2, x.z1_3, x.up_4, x.i64_5, x.f64_6, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.sl_0, x.u8_1, x.n2_2, x.z1_3, x.up_4, x.i64_5, x.f64_6, x.post)
}

var gT = T{sl_0: []int{1, 2, 3}, u8_1: 200, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_3: struct{}{}, up_4: 77, i64_5: -9000000000, f64_6: -2.25}
var gO = O{pre: 7, T: T{sl_0: []int{1, 2, 3}, u8_1: 200, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_3: struct{}{}, up_4: 77, i64_5: -9000000000, f64_6: -2.25}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{sl_0: []int{1, 2, 3}, u8_1: 200, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_3: struct{}{}, up_4: 77, i64_5: -9000000000, f64_6: -2.25}, {}, T{sl_0: []int{9}, u8_1: 13, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, z1_3: struct{}{}, up_4: 88, i64_5: 8000000000, f64_6: 1e100}}
var sT = []T{T{sl_0: []int{9}, u8_1: 13, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, z1_3: struct{}{}, up_4: 88, i64_5: 8000000000, f64_6: 1e100}, T{sl_0: []int{1, 2, 3}, u8_1: 200, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_3: struct{}{}, up_4: 77, i64_5: -9000000000, f64_6: -2.25}}
var aO = [2]O{O{pre: 1, T: T{sl_0: []int{1, 2, 3}, u8_1: 200, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_3: struct{}{}, up_4: 77, i64_5: -9000000000, f64_6: -2.25}, post: 2}, O{pre: 3, T: T{sl_0: []int{9}, u8_1: 13, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, z1_3: struct{}{}, up_4: 88, i64_5: 8000000000, f64_6: 1e100}, post: 4}}
var mT = map[int]T{}
var pField = &gT.u8_1
var pFieldO = &gO.u8_1
var pElem = &aT[2].u8_1
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.sl_0 = []int{9}
		p.u8_1 = 13
		p.n2_2 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.z1_3 = struct{}{}
		p.up_4 = 88
		p.i64_5 = 8000000000
		p.f64_6 = 1e100
	} else {
		p.sl_0 = []int{1, 2, 3}
		p.u8_1 = 200
		p.n2_2 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.z1_3 = struct{}{}
		p.up_4 = 77
		p.i64_5 = -9000000000
		p.f64_6 = -2.25
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("sl_0", unsafe.Offsetof(t.sl_0), unsafe.Sizeof(t.sl_0), unsafe.Alignof(t.sl_0))
	fmt.Println("u8_1", unsafe.Offsetof(t.u8_1), unsafe.Sizeof(t.u8_1), unsafe.Alignof(t.u8_1))
	fmt.Println("n2_2", unsafe.Offsetof(t.n2_2), unsafe.Sizeof(t.n2_2), unsafe.Alignof(t.n2_2))
	fmt.Println("z1_3", unsafe.Offsetof(t.z1_3), unsafe.Sizeof(t.z1_3), unsafe.Alignof(t.z1_3))
	fmt.Println("up_4", unsafe.Offsetof(t.up_4), unsafe.Sizeof(t.up_4), unsafe.Alignof(t.up_4))
	fmt.Println("i64_5", unsafe.Offsetof(t.i64_5), unsafe.Sizeof(t.i64_5), unsafe.Alignof(t.i64_5))
	fmt.Println("f64_6", unsafe.Offsetof(t.f64_6), unsafe.Sizeof(t.f64_6), unsafe.Alignof(t.f64_6))
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
	fmt.Println(*(*uint8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u8_1))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_4))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_5))))
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_6))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.sl_0, gT.u8_1, gT.n2_2, gT.z1_3, gT.up_4, gT.i64_5, gT.f64_6)
	fmt.Println("directO", gO.pre, gO.sl_0, gO.u8_1, gO.n2_2, gO.z1_3, gO.up_4, gO.i64_5, gO.f64_6, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{sl_0: []int{1, 2, 3}, u8_1: 200, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, z1_3: struct{}{}, up_4: 77, i64_5: -9000000000, f64_6: -2.25}
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
	loc := T{sl_0: []int{9}, u8_1: 13, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, z1_3: struct{}{}, up_4: 88, i64_5: 8000000000, f64_6: 1e100}
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
			res[i] = fmt.Sprint(v.sl_0, v.u8_1, v.n2_2, v.z1_3, v.up_4, v.i64_5, v.f64_6, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
