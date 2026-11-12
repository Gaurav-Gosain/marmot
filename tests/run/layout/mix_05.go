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
	z0_0  [0]int
	f32_1 float32
	u8_2  uint8
	c64_3 complex64
	b_4   bool
	i64_5 int64
	u64_6 uint64
	up_7  uintptr
	u64_8 uint64
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.z0_0, x.f32_1, x.u8_2, x.c64_3, x.b_4, x.i64_5, x.u64_6, x.up_7, x.u64_8)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.z0_0, x.f32_1, x.u8_2, x.c64_3, x.b_4, x.i64_5, x.u64_6, x.up_7, x.u64_8)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.z0_0, x.in.f32_1, x.in.u8_2, x.in.c64_3, x.in.b_4, x.in.i64_5, x.in.u64_6, x.in.up_7, x.in.u64_8, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.z0_0, x.in.f32_1, x.in.u8_2, x.in.c64_3, x.in.b_4, x.in.i64_5, x.in.u64_6, x.in.up_7, x.in.u64_8, x.post)
}

var gT = T{z0_0: [0]int{}, f32_1: 1.5, u8_2: 200, c64_3: 1 + 2i, b_4: true, i64_5: -9000000000, u64_6: 18000000000000000000, up_7: 77, u64_8: 18000000000000000000}
var gO = O{pre: 7, in: T{z0_0: [0]int{}, f32_1: 1.5, u8_2: 200, c64_3: 1 + 2i, b_4: true, i64_5: -9000000000, u64_6: 18000000000000000000, up_7: 77, u64_8: 18000000000000000000}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{z0_0: [0]int{}, f32_1: 1.5, u8_2: 200, c64_3: 1 + 2i, b_4: true, i64_5: -9000000000, u64_6: 18000000000000000000, up_7: 77, u64_8: 18000000000000000000}, {}, T{z0_0: [0]int{}, f32_1: -2.75, u8_2: 13, c64_3: -3 - 4i, b_4: false, i64_5: 8000000000, u64_6: 77, up_7: 88, u64_8: 77}}
var sT = []T{T{z0_0: [0]int{}, f32_1: -2.75, u8_2: 13, c64_3: -3 - 4i, b_4: false, i64_5: 8000000000, u64_6: 77, up_7: 88, u64_8: 77}, T{z0_0: [0]int{}, f32_1: 1.5, u8_2: 200, c64_3: 1 + 2i, b_4: true, i64_5: -9000000000, u64_6: 18000000000000000000, up_7: 77, u64_8: 18000000000000000000}}
var aO = [2]O{O{pre: 1, in: T{z0_0: [0]int{}, f32_1: 1.5, u8_2: 200, c64_3: 1 + 2i, b_4: true, i64_5: -9000000000, u64_6: 18000000000000000000, up_7: 77, u64_8: 18000000000000000000}, post: 2}, O{pre: 3, in: T{z0_0: [0]int{}, f32_1: -2.75, u8_2: 13, c64_3: -3 - 4i, b_4: false, i64_5: 8000000000, u64_6: 77, up_7: 88, u64_8: 77}, post: 4}}
var mT = map[int]T{}
var pField = &gT.u8_2
var pFieldO = &gO.in.u8_2
var pElem = &aT[2].u8_2
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.z0_0 = [0]int{}
		p.f32_1 = -2.75
		p.u8_2 = 13
		p.c64_3 = -3 - 4i
		p.b_4 = false
		p.i64_5 = 8000000000
		p.u64_6 = 77
		p.up_7 = 88
		p.u64_8 = 77
	} else {
		p.z0_0 = [0]int{}
		p.f32_1 = 1.5
		p.u8_2 = 200
		p.c64_3 = 1 + 2i
		p.b_4 = true
		p.i64_5 = -9000000000
		p.u64_6 = 18000000000000000000
		p.up_7 = 77
		p.u64_8 = 18000000000000000000
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("z0_0", unsafe.Offsetof(t.z0_0), unsafe.Sizeof(t.z0_0), unsafe.Alignof(t.z0_0))
	fmt.Println("f32_1", unsafe.Offsetof(t.f32_1), unsafe.Sizeof(t.f32_1), unsafe.Alignof(t.f32_1))
	fmt.Println("u8_2", unsafe.Offsetof(t.u8_2), unsafe.Sizeof(t.u8_2), unsafe.Alignof(t.u8_2))
	fmt.Println("c64_3", unsafe.Offsetof(t.c64_3), unsafe.Sizeof(t.c64_3), unsafe.Alignof(t.c64_3))
	fmt.Println("b_4", unsafe.Offsetof(t.b_4), unsafe.Sizeof(t.b_4), unsafe.Alignof(t.b_4))
	fmt.Println("i64_5", unsafe.Offsetof(t.i64_5), unsafe.Sizeof(t.i64_5), unsafe.Alignof(t.i64_5))
	fmt.Println("u64_6", unsafe.Offsetof(t.u64_6), unsafe.Sizeof(t.u64_6), unsafe.Alignof(t.u64_6))
	fmt.Println("up_7", unsafe.Offsetof(t.up_7), unsafe.Sizeof(t.up_7), unsafe.Alignof(t.up_7))
	fmt.Println("u64_8", unsafe.Offsetof(t.u64_8), unsafe.Sizeof(t.u64_8), unsafe.Alignof(t.u64_8))
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
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_1))))
	fmt.Println(*(*uint8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u8_2))))
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_4))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_5))))
	fmt.Println(*(*uint64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u64_6))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_7))))
	fmt.Println(*(*uint64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u64_8))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.z0_0, gT.f32_1, gT.u8_2, gT.c64_3, gT.b_4, gT.i64_5, gT.u64_6, gT.up_7, gT.u64_8)
	fmt.Println("directO", gO.pre, gO.in.z0_0, gO.in.f32_1, gO.in.u8_2, gO.in.c64_3, gO.in.b_4, gO.in.i64_5, gO.in.u64_6, gO.in.up_7, gO.in.u64_8, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{z0_0: [0]int{}, f32_1: 1.5, u8_2: 200, c64_3: 1 + 2i, b_4: true, i64_5: -9000000000, u64_6: 18000000000000000000, up_7: 77, u64_8: 18000000000000000000}
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
	loc := T{z0_0: [0]int{}, f32_1: -2.75, u8_2: 13, c64_3: -3 - 4i, b_4: false, i64_5: 8000000000, u64_6: 77, up_7: 88, u64_8: 77}
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
	fmt.Println(cp == gT, gT == T{z0_0: [0]int{}, f32_1: 1.5, u8_2: 200, c64_3: 1 + 2i, b_4: true, i64_5: -9000000000, u64_6: 18000000000000000000, up_7: 77, u64_8: 18000000000000000000}, dT == gT)
	cp.u64_8 = 77
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
			res[i] = fmt.Sprint(v.z0_0, v.f32_1, v.u8_2, v.c64_3, v.b_4, v.i64_5, v.u64_6, v.up_7, v.u64_8, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
