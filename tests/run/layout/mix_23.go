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
	c128_0 complex128
	f64_1  float64
	u32_2  uint32
	sl_3   []int
	z1_4   struct{}
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.c128_0, x.f64_1, x.u32_2, x.sl_3, x.z1_4)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.c128_0, x.f64_1, x.u32_2, x.sl_3, x.z1_4)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.c128_0, x.in.f64_1, x.in.u32_2, x.in.sl_3, x.in.z1_4, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.c128_0, x.in.f64_1, x.in.u32_2, x.in.sl_3, x.in.z1_4, x.post)
}

var gT = T{c128_0: 3.5 - 1i, f64_1: -2.25, u32_2: 4000000000, sl_3: []int{1, 2, 3}, z1_4: struct{}{}}
var gO = O{pre: 7, in: T{c128_0: 3.5 - 1i, f64_1: -2.25, u32_2: 4000000000, sl_3: []int{1, 2, 3}, z1_4: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{c128_0: 3.5 - 1i, f64_1: -2.25, u32_2: 4000000000, sl_3: []int{1, 2, 3}, z1_4: struct{}{}}, {}, T{c128_0: 0.5 + 0.25i, f64_1: 1e100, u32_2: 99, sl_3: []int{9}, z1_4: struct{}{}}}
var sT = []T{T{c128_0: 0.5 + 0.25i, f64_1: 1e100, u32_2: 99, sl_3: []int{9}, z1_4: struct{}{}}, T{c128_0: 3.5 - 1i, f64_1: -2.25, u32_2: 4000000000, sl_3: []int{1, 2, 3}, z1_4: struct{}{}}}
var aO = [2]O{O{pre: 1, in: T{c128_0: 3.5 - 1i, f64_1: -2.25, u32_2: 4000000000, sl_3: []int{1, 2, 3}, z1_4: struct{}{}}, post: 2}, O{pre: 3, in: T{c128_0: 0.5 + 0.25i, f64_1: 1e100, u32_2: 99, sl_3: []int{9}, z1_4: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.f64_1
var pFieldO = &gO.in.f64_1
var pElem = &aT[2].f64_1
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.c128_0 = 0.5 + 0.25i
		p.f64_1 = 1e100
		p.u32_2 = 99
		p.sl_3 = []int{9}
		p.z1_4 = struct{}{}
	} else {
		p.c128_0 = 3.5 - 1i
		p.f64_1 = -2.25
		p.u32_2 = 4000000000
		p.sl_3 = []int{1, 2, 3}
		p.z1_4 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("c128_0", unsafe.Offsetof(t.c128_0), unsafe.Sizeof(t.c128_0), unsafe.Alignof(t.c128_0))
	fmt.Println("f64_1", unsafe.Offsetof(t.f64_1), unsafe.Sizeof(t.f64_1), unsafe.Alignof(t.f64_1))
	fmt.Println("u32_2", unsafe.Offsetof(t.u32_2), unsafe.Sizeof(t.u32_2), unsafe.Alignof(t.u32_2))
	fmt.Println("sl_3", unsafe.Offsetof(t.sl_3), unsafe.Sizeof(t.sl_3), unsafe.Alignof(t.sl_3))
	fmt.Println("z1_4", unsafe.Offsetof(t.z1_4), unsafe.Sizeof(t.z1_4), unsafe.Alignof(t.z1_4))
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
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_1))))
	fmt.Println(*(*uint32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u32_2))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.c128_0, gT.f64_1, gT.u32_2, gT.sl_3, gT.z1_4)
	fmt.Println("directO", gO.pre, gO.in.c128_0, gO.in.f64_1, gO.in.u32_2, gO.in.sl_3, gO.in.z1_4, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{c128_0: 3.5 - 1i, f64_1: -2.25, u32_2: 4000000000, sl_3: []int{1, 2, 3}, z1_4: struct{}{}}
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
	loc := T{c128_0: 0.5 + 0.25i, f64_1: 1e100, u32_2: 99, sl_3: []int{9}, z1_4: struct{}{}}
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
			res[i] = fmt.Sprint(v.c128_0, v.f64_1, v.u32_2, v.sl_3, v.z1_4, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
