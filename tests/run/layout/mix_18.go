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
	i64_0 int64
	f32_1 float32
	z0_2  [0]int
	a3_3  [3]byte
	c64_4 complex64
	f64_5 float64
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.i64_0, x.f32_1, x.z0_2, x.a3_3, x.c64_4, x.f64_5)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.i64_0, x.f32_1, x.z0_2, x.a3_3, x.c64_4, x.f64_5)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.i64_0, x.in.f32_1, x.in.z0_2, x.in.a3_3, x.in.c64_4, x.in.f64_5, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.i64_0, x.in.f32_1, x.in.z0_2, x.in.a3_3, x.in.c64_4, x.in.f64_5, x.post)
}

var gT = T{i64_0: -9000000000, f32_1: 1.5, z0_2: [0]int{}, a3_3: [3]byte{1, 2, 3}, c64_4: 1 + 2i, f64_5: -2.25}
var gO = O{pre: 7, in: T{i64_0: -9000000000, f32_1: 1.5, z0_2: [0]int{}, a3_3: [3]byte{1, 2, 3}, c64_4: 1 + 2i, f64_5: -2.25}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{i64_0: -9000000000, f32_1: 1.5, z0_2: [0]int{}, a3_3: [3]byte{1, 2, 3}, c64_4: 1 + 2i, f64_5: -2.25}, {}, T{i64_0: 8000000000, f32_1: -2.75, z0_2: [0]int{}, a3_3: [3]byte{7, 8, 9}, c64_4: -3 - 4i, f64_5: 1e100}}
var sT = []T{T{i64_0: 8000000000, f32_1: -2.75, z0_2: [0]int{}, a3_3: [3]byte{7, 8, 9}, c64_4: -3 - 4i, f64_5: 1e100}, T{i64_0: -9000000000, f32_1: 1.5, z0_2: [0]int{}, a3_3: [3]byte{1, 2, 3}, c64_4: 1 + 2i, f64_5: -2.25}}
var aO = [2]O{O{pre: 1, in: T{i64_0: -9000000000, f32_1: 1.5, z0_2: [0]int{}, a3_3: [3]byte{1, 2, 3}, c64_4: 1 + 2i, f64_5: -2.25}, post: 2}, O{pre: 3, in: T{i64_0: 8000000000, f32_1: -2.75, z0_2: [0]int{}, a3_3: [3]byte{7, 8, 9}, c64_4: -3 - 4i, f64_5: 1e100}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i64_0
var pFieldO = &gO.in.i64_0
var pElem = &aT[2].i64_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.i64_0 = 8000000000
		p.f32_1 = -2.75
		p.z0_2 = [0]int{}
		p.a3_3 = [3]byte{7, 8, 9}
		p.c64_4 = -3 - 4i
		p.f64_5 = 1e100
	} else {
		p.i64_0 = -9000000000
		p.f32_1 = 1.5
		p.z0_2 = [0]int{}
		p.a3_3 = [3]byte{1, 2, 3}
		p.c64_4 = 1 + 2i
		p.f64_5 = -2.25
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("i64_0", unsafe.Offsetof(t.i64_0), unsafe.Sizeof(t.i64_0), unsafe.Alignof(t.i64_0))
	fmt.Println("f32_1", unsafe.Offsetof(t.f32_1), unsafe.Sizeof(t.f32_1), unsafe.Alignof(t.f32_1))
	fmt.Println("z0_2", unsafe.Offsetof(t.z0_2), unsafe.Sizeof(t.z0_2), unsafe.Alignof(t.z0_2))
	fmt.Println("a3_3", unsafe.Offsetof(t.a3_3), unsafe.Sizeof(t.a3_3), unsafe.Alignof(t.a3_3))
	fmt.Println("c64_4", unsafe.Offsetof(t.c64_4), unsafe.Sizeof(t.c64_4), unsafe.Alignof(t.c64_4))
	fmt.Println("f64_5", unsafe.Offsetof(t.f64_5), unsafe.Sizeof(t.f64_5), unsafe.Alignof(t.f64_5))
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
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_0))))
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_1))))
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_5))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.i64_0, gT.f32_1, gT.z0_2, gT.a3_3, gT.c64_4, gT.f64_5)
	fmt.Println("directO", gO.pre, gO.in.i64_0, gO.in.f32_1, gO.in.z0_2, gO.in.a3_3, gO.in.c64_4, gO.in.f64_5, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{i64_0: -9000000000, f32_1: 1.5, z0_2: [0]int{}, a3_3: [3]byte{1, 2, 3}, c64_4: 1 + 2i, f64_5: -2.25}
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
	loc := T{i64_0: 8000000000, f32_1: -2.75, z0_2: [0]int{}, a3_3: [3]byte{7, 8, 9}, c64_4: -3 - 4i, f64_5: 1e100}
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
	fmt.Println(cp == gT, gT == T{i64_0: -9000000000, f32_1: 1.5, z0_2: [0]int{}, a3_3: [3]byte{1, 2, 3}, c64_4: 1 + 2i, f64_5: -2.25}, dT == gT)
	cp.f64_5 = 1e100
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
			res[i] = fmt.Sprint(v.i64_0, v.f32_1, v.z0_2, v.a3_3, v.c64_4, v.f64_5, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
