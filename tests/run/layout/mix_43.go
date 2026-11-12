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
	f32_0 float32
	i32_1 int32
	u8_2  uint8
	u16_3 uint16
	z1_4  struct{}
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.f32_0, x.i32_1, x.u8_2, x.u16_3, x.z1_4)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.f32_0, x.i32_1, x.u8_2, x.u16_3, x.z1_4)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.f32_0, x.i32_1, x.u8_2, x.u16_3, x.z1_4, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.f32_0, x.i32_1, x.u8_2, x.u16_3, x.z1_4, x.post)
}

var gT = T{f32_0: 1.5, i32_1: -100000, u8_2: 200, u16_3: 60000, z1_4: struct{}{}}
var gO = O{pre: 7, T: T{f32_0: 1.5, i32_1: -100000, u8_2: 200, u16_3: 60000, z1_4: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{f32_0: 1.5, i32_1: -100000, u8_2: 200, u16_3: 60000, z1_4: struct{}{}}, {}, T{f32_0: -2.75, i32_1: 70000, u8_2: 13, u16_3: 513, z1_4: struct{}{}}}
var sT = []T{T{f32_0: -2.75, i32_1: 70000, u8_2: 13, u16_3: 513, z1_4: struct{}{}}, T{f32_0: 1.5, i32_1: -100000, u8_2: 200, u16_3: 60000, z1_4: struct{}{}}}
var aO = [2]O{O{pre: 1, T: T{f32_0: 1.5, i32_1: -100000, u8_2: 200, u16_3: 60000, z1_4: struct{}{}}, post: 2}, O{pre: 3, T: T{f32_0: -2.75, i32_1: 70000, u8_2: 13, u16_3: 513, z1_4: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i32_1
var pFieldO = &gO.i32_1
var pElem = &aT[2].i32_1
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.f32_0 = -2.75
		p.i32_1 = 70000
		p.u8_2 = 13
		p.u16_3 = 513
		p.z1_4 = struct{}{}
	} else {
		p.f32_0 = 1.5
		p.i32_1 = -100000
		p.u8_2 = 200
		p.u16_3 = 60000
		p.z1_4 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("f32_0", unsafe.Offsetof(t.f32_0), unsafe.Sizeof(t.f32_0), unsafe.Alignof(t.f32_0))
	fmt.Println("i32_1", unsafe.Offsetof(t.i32_1), unsafe.Sizeof(t.i32_1), unsafe.Alignof(t.i32_1))
	fmt.Println("u8_2", unsafe.Offsetof(t.u8_2), unsafe.Sizeof(t.u8_2), unsafe.Alignof(t.u8_2))
	fmt.Println("u16_3", unsafe.Offsetof(t.u16_3), unsafe.Sizeof(t.u16_3), unsafe.Alignof(t.u16_3))
	fmt.Println("z1_4", unsafe.Offsetof(t.z1_4), unsafe.Sizeof(t.z1_4), unsafe.Alignof(t.z1_4))
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
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_0))))
	fmt.Println(*(*int32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i32_1))))
	fmt.Println(*(*uint8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u8_2))))
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_3))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.f32_0, gT.i32_1, gT.u8_2, gT.u16_3, gT.z1_4)
	fmt.Println("directO", gO.pre, gO.f32_0, gO.i32_1, gO.u8_2, gO.u16_3, gO.z1_4, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{f32_0: 1.5, i32_1: -100000, u8_2: 200, u16_3: 60000, z1_4: struct{}{}}
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
	loc := T{f32_0: -2.75, i32_1: 70000, u8_2: 13, u16_3: 513, z1_4: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{f32_0: 1.5, i32_1: -100000, u8_2: 200, u16_3: 60000, z1_4: struct{}{}}, dT == gT)
	cp.z1_4 = struct{}{}
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
			res[i] = fmt.Sprint(v.f32_0, v.i32_1, v.u8_2, v.u16_3, v.z1_4, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
