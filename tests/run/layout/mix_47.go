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
	a3_0  [3]byte
	i64_1 int64
	n1_2  N1
	a3_3  [3]byte
	f64_4 float64
	sl_5  []int
	up_6  uintptr
	u32_7 uint32
	z0_8  [0]int
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.a3_0, x.i64_1, x.n1_2, x.a3_3, x.f64_4, x.sl_5, x.up_6, x.u32_7, x.z0_8)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.a3_0, x.i64_1, x.n1_2, x.a3_3, x.f64_4, x.sl_5, x.up_6, x.u32_7, x.z0_8)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.a3_0, x.in.i64_1, x.in.n1_2, x.in.a3_3, x.in.f64_4, x.in.sl_5, x.in.up_6, x.in.u32_7, x.in.z0_8, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.a3_0, x.in.i64_1, x.in.n1_2, x.in.a3_3, x.in.f64_4, x.in.sl_5, x.in.up_6, x.in.u32_7, x.in.z0_8, x.post)
}

var gT = T{a3_0: [3]byte{1, 2, 3}, i64_1: -9000000000, n1_2: N1{3, -7}, a3_3: [3]byte{1, 2, 3}, f64_4: -2.25, sl_5: []int{1, 2, 3}, up_6: 77, u32_7: 4000000000, z0_8: [0]int{}}
var gO = O{pre: 7, in: T{a3_0: [3]byte{1, 2, 3}, i64_1: -9000000000, n1_2: N1{3, -7}, a3_3: [3]byte{1, 2, 3}, f64_4: -2.25, sl_5: []int{1, 2, 3}, up_6: 77, u32_7: 4000000000, z0_8: [0]int{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{a3_0: [3]byte{1, 2, 3}, i64_1: -9000000000, n1_2: N1{3, -7}, a3_3: [3]byte{1, 2, 3}, f64_4: -2.25, sl_5: []int{1, 2, 3}, up_6: 77, u32_7: 4000000000, z0_8: [0]int{}}, {}, T{a3_0: [3]byte{7, 8, 9}, i64_1: 8000000000, n1_2: N1{-3, 70000000000}, a3_3: [3]byte{7, 8, 9}, f64_4: 1e100, sl_5: []int{9}, up_6: 88, u32_7: 99, z0_8: [0]int{}}}
var sT = []T{T{a3_0: [3]byte{7, 8, 9}, i64_1: 8000000000, n1_2: N1{-3, 70000000000}, a3_3: [3]byte{7, 8, 9}, f64_4: 1e100, sl_5: []int{9}, up_6: 88, u32_7: 99, z0_8: [0]int{}}, T{a3_0: [3]byte{1, 2, 3}, i64_1: -9000000000, n1_2: N1{3, -7}, a3_3: [3]byte{1, 2, 3}, f64_4: -2.25, sl_5: []int{1, 2, 3}, up_6: 77, u32_7: 4000000000, z0_8: [0]int{}}}
var aO = [2]O{O{pre: 1, in: T{a3_0: [3]byte{1, 2, 3}, i64_1: -9000000000, n1_2: N1{3, -7}, a3_3: [3]byte{1, 2, 3}, f64_4: -2.25, sl_5: []int{1, 2, 3}, up_6: 77, u32_7: 4000000000, z0_8: [0]int{}}, post: 2}, O{pre: 3, in: T{a3_0: [3]byte{7, 8, 9}, i64_1: 8000000000, n1_2: N1{-3, 70000000000}, a3_3: [3]byte{7, 8, 9}, f64_4: 1e100, sl_5: []int{9}, up_6: 88, u32_7: 99, z0_8: [0]int{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i64_1
var pFieldO = &gO.in.i64_1
var pElem = &aT[2].i64_1
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.a3_0 = [3]byte{7, 8, 9}
		p.i64_1 = 8000000000
		p.n1_2 = N1{-3, 70000000000}
		p.a3_3 = [3]byte{7, 8, 9}
		p.f64_4 = 1e100
		p.sl_5 = []int{9}
		p.up_6 = 88
		p.u32_7 = 99
		p.z0_8 = [0]int{}
	} else {
		p.a3_0 = [3]byte{1, 2, 3}
		p.i64_1 = -9000000000
		p.n1_2 = N1{3, -7}
		p.a3_3 = [3]byte{1, 2, 3}
		p.f64_4 = -2.25
		p.sl_5 = []int{1, 2, 3}
		p.up_6 = 77
		p.u32_7 = 4000000000
		p.z0_8 = [0]int{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("a3_0", unsafe.Offsetof(t.a3_0), unsafe.Sizeof(t.a3_0), unsafe.Alignof(t.a3_0))
	fmt.Println("i64_1", unsafe.Offsetof(t.i64_1), unsafe.Sizeof(t.i64_1), unsafe.Alignof(t.i64_1))
	fmt.Println("n1_2", unsafe.Offsetof(t.n1_2), unsafe.Sizeof(t.n1_2), unsafe.Alignof(t.n1_2))
	fmt.Println("a3_3", unsafe.Offsetof(t.a3_3), unsafe.Sizeof(t.a3_3), unsafe.Alignof(t.a3_3))
	fmt.Println("f64_4", unsafe.Offsetof(t.f64_4), unsafe.Sizeof(t.f64_4), unsafe.Alignof(t.f64_4))
	fmt.Println("sl_5", unsafe.Offsetof(t.sl_5), unsafe.Sizeof(t.sl_5), unsafe.Alignof(t.sl_5))
	fmt.Println("up_6", unsafe.Offsetof(t.up_6), unsafe.Sizeof(t.up_6), unsafe.Alignof(t.up_6))
	fmt.Println("u32_7", unsafe.Offsetof(t.u32_7), unsafe.Sizeof(t.u32_7), unsafe.Alignof(t.u32_7))
	fmt.Println("z0_8", unsafe.Offsetof(t.z0_8), unsafe.Sizeof(t.z0_8), unsafe.Alignof(t.z0_8))
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
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_1))))
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_4))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_6))))
	fmt.Println(*(*uint32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u32_7))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.a3_0, gT.i64_1, gT.n1_2, gT.a3_3, gT.f64_4, gT.sl_5, gT.up_6, gT.u32_7, gT.z0_8)
	fmt.Println("directO", gO.pre, gO.in.a3_0, gO.in.i64_1, gO.in.n1_2, gO.in.a3_3, gO.in.f64_4, gO.in.sl_5, gO.in.up_6, gO.in.u32_7, gO.in.z0_8, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{a3_0: [3]byte{1, 2, 3}, i64_1: -9000000000, n1_2: N1{3, -7}, a3_3: [3]byte{1, 2, 3}, f64_4: -2.25, sl_5: []int{1, 2, 3}, up_6: 77, u32_7: 4000000000, z0_8: [0]int{}}
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
	loc := T{a3_0: [3]byte{7, 8, 9}, i64_1: 8000000000, n1_2: N1{-3, 70000000000}, a3_3: [3]byte{7, 8, 9}, f64_4: 1e100, sl_5: []int{9}, up_6: 88, u32_7: 99, z0_8: [0]int{}}
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
			res[i] = fmt.Sprint(v.a3_0, v.i64_1, v.n1_2, v.a3_3, v.f64_4, v.sl_5, v.up_6, v.u32_7, v.z0_8, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
