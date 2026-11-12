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
	i_0    int
	f32_1  float32
	i16_2  int16
	c128_3 complex128
	z2_4   [0]int64
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.i_0, x.f32_1, x.i16_2, x.c128_3, x.z2_4)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.i_0, x.f32_1, x.i16_2, x.c128_3, x.z2_4)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.i_0, x.in.f32_1, x.in.i16_2, x.in.c128_3, x.in.z2_4, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.i_0, x.in.f32_1, x.in.i16_2, x.in.c128_3, x.in.z2_4, x.post)
}

var gT = T{i_0: -42, f32_1: 1.5, i16_2: -1234, c128_3: 3.5 - 1i, z2_4: [0]int64{}}
var gO = O{pre: 7, in: T{i_0: -42, f32_1: 1.5, i16_2: -1234, c128_3: 3.5 - 1i, z2_4: [0]int64{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{i_0: -42, f32_1: 1.5, i16_2: -1234, c128_3: 3.5 - 1i, z2_4: [0]int64{}}, {}, T{i_0: 123456789, f32_1: -2.75, i16_2: 4321, c128_3: 0.5 + 0.25i, z2_4: [0]int64{}}}
var sT = []T{T{i_0: 123456789, f32_1: -2.75, i16_2: 4321, c128_3: 0.5 + 0.25i, z2_4: [0]int64{}}, T{i_0: -42, f32_1: 1.5, i16_2: -1234, c128_3: 3.5 - 1i, z2_4: [0]int64{}}}
var aO = [2]O{O{pre: 1, in: T{i_0: -42, f32_1: 1.5, i16_2: -1234, c128_3: 3.5 - 1i, z2_4: [0]int64{}}, post: 2}, O{pre: 3, in: T{i_0: 123456789, f32_1: -2.75, i16_2: 4321, c128_3: 0.5 + 0.25i, z2_4: [0]int64{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i_0
var pFieldO = &gO.in.i_0
var pElem = &aT[2].i_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.i_0 = 123456789
		p.f32_1 = -2.75
		p.i16_2 = 4321
		p.c128_3 = 0.5 + 0.25i
		p.z2_4 = [0]int64{}
	} else {
		p.i_0 = -42
		p.f32_1 = 1.5
		p.i16_2 = -1234
		p.c128_3 = 3.5 - 1i
		p.z2_4 = [0]int64{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("i_0", unsafe.Offsetof(t.i_0), unsafe.Sizeof(t.i_0), unsafe.Alignof(t.i_0))
	fmt.Println("f32_1", unsafe.Offsetof(t.f32_1), unsafe.Sizeof(t.f32_1), unsafe.Alignof(t.f32_1))
	fmt.Println("i16_2", unsafe.Offsetof(t.i16_2), unsafe.Sizeof(t.i16_2), unsafe.Alignof(t.i16_2))
	fmt.Println("c128_3", unsafe.Offsetof(t.c128_3), unsafe.Sizeof(t.c128_3), unsafe.Alignof(t.c128_3))
	fmt.Println("z2_4", unsafe.Offsetof(t.z2_4), unsafe.Sizeof(t.z2_4), unsafe.Alignof(t.z2_4))
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
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_1))))
	fmt.Println(*(*int16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i16_2))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.i_0, gT.f32_1, gT.i16_2, gT.c128_3, gT.z2_4)
	fmt.Println("directO", gO.pre, gO.in.i_0, gO.in.f32_1, gO.in.i16_2, gO.in.c128_3, gO.in.z2_4, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{i_0: -42, f32_1: 1.5, i16_2: -1234, c128_3: 3.5 - 1i, z2_4: [0]int64{}}
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
	loc := T{i_0: 123456789, f32_1: -2.75, i16_2: 4321, c128_3: 0.5 + 0.25i, z2_4: [0]int64{}}
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
	fmt.Println(cp == gT, gT == T{i_0: -42, f32_1: 1.5, i16_2: -1234, c128_3: 3.5 - 1i, z2_4: [0]int64{}}, dT == gT)
	cp.z2_4 = [0]int64{}
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
			res[i] = fmt.Sprint(v.i_0, v.f32_1, v.i16_2, v.c128_3, v.z2_4, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
