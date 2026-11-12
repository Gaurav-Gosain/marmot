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
	f32_2 float32
	u8_3  uint8
	a2_4  [2]int16
	r_5   rune
	a3_6  [3]byte
	c64_7 complex64
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.z0_0, x.f32_1, x.f32_2, x.u8_3, x.a2_4, x.r_5, x.a3_6, x.c64_7)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.z0_0, x.f32_1, x.f32_2, x.u8_3, x.a2_4, x.r_5, x.a3_6, x.c64_7)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.z0_0, x.in.f32_1, x.in.f32_2, x.in.u8_3, x.in.a2_4, x.in.r_5, x.in.a3_6, x.in.c64_7, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.z0_0, x.in.f32_1, x.in.f32_2, x.in.u8_3, x.in.a2_4, x.in.r_5, x.in.a3_6, x.in.c64_7, x.post)
}

var gT = T{z0_0: [0]int{}, f32_1: 1.5, f32_2: 1.5, u8_3: 200, a2_4: [2]int16{-1, 2}, r_5: 'x', a3_6: [3]byte{1, 2, 3}, c64_7: 1 + 2i}
var gO = O{pre: 7, in: T{z0_0: [0]int{}, f32_1: 1.5, f32_2: 1.5, u8_3: 200, a2_4: [2]int16{-1, 2}, r_5: 'x', a3_6: [3]byte{1, 2, 3}, c64_7: 1 + 2i}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{z0_0: [0]int{}, f32_1: 1.5, f32_2: 1.5, u8_3: 200, a2_4: [2]int16{-1, 2}, r_5: 'x', a3_6: [3]byte{1, 2, 3}, c64_7: 1 + 2i}, {}, T{z0_0: [0]int{}, f32_1: -2.75, f32_2: -2.75, u8_3: 13, a2_4: [2]int16{300, -300}, r_5: '世', a3_6: [3]byte{7, 8, 9}, c64_7: -3 - 4i}}
var sT = []T{T{z0_0: [0]int{}, f32_1: -2.75, f32_2: -2.75, u8_3: 13, a2_4: [2]int16{300, -300}, r_5: '世', a3_6: [3]byte{7, 8, 9}, c64_7: -3 - 4i}, T{z0_0: [0]int{}, f32_1: 1.5, f32_2: 1.5, u8_3: 200, a2_4: [2]int16{-1, 2}, r_5: 'x', a3_6: [3]byte{1, 2, 3}, c64_7: 1 + 2i}}
var aO = [2]O{O{pre: 1, in: T{z0_0: [0]int{}, f32_1: 1.5, f32_2: 1.5, u8_3: 200, a2_4: [2]int16{-1, 2}, r_5: 'x', a3_6: [3]byte{1, 2, 3}, c64_7: 1 + 2i}, post: 2}, O{pre: 3, in: T{z0_0: [0]int{}, f32_1: -2.75, f32_2: -2.75, u8_3: 13, a2_4: [2]int16{300, -300}, r_5: '世', a3_6: [3]byte{7, 8, 9}, c64_7: -3 - 4i}, post: 4}}
var mT = map[int]T{}
var pField = &gT.u8_3
var pFieldO = &gO.in.u8_3
var pElem = &aT[2].u8_3
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.z0_0 = [0]int{}
		p.f32_1 = -2.75
		p.f32_2 = -2.75
		p.u8_3 = 13
		p.a2_4 = [2]int16{300, -300}
		p.r_5 = '世'
		p.a3_6 = [3]byte{7, 8, 9}
		p.c64_7 = -3 - 4i
	} else {
		p.z0_0 = [0]int{}
		p.f32_1 = 1.5
		p.f32_2 = 1.5
		p.u8_3 = 200
		p.a2_4 = [2]int16{-1, 2}
		p.r_5 = 'x'
		p.a3_6 = [3]byte{1, 2, 3}
		p.c64_7 = 1 + 2i
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("z0_0", unsafe.Offsetof(t.z0_0), unsafe.Sizeof(t.z0_0), unsafe.Alignof(t.z0_0))
	fmt.Println("f32_1", unsafe.Offsetof(t.f32_1), unsafe.Sizeof(t.f32_1), unsafe.Alignof(t.f32_1))
	fmt.Println("f32_2", unsafe.Offsetof(t.f32_2), unsafe.Sizeof(t.f32_2), unsafe.Alignof(t.f32_2))
	fmt.Println("u8_3", unsafe.Offsetof(t.u8_3), unsafe.Sizeof(t.u8_3), unsafe.Alignof(t.u8_3))
	fmt.Println("a2_4", unsafe.Offsetof(t.a2_4), unsafe.Sizeof(t.a2_4), unsafe.Alignof(t.a2_4))
	fmt.Println("r_5", unsafe.Offsetof(t.r_5), unsafe.Sizeof(t.r_5), unsafe.Alignof(t.r_5))
	fmt.Println("a3_6", unsafe.Offsetof(t.a3_6), unsafe.Sizeof(t.a3_6), unsafe.Alignof(t.a3_6))
	fmt.Println("c64_7", unsafe.Offsetof(t.c64_7), unsafe.Sizeof(t.c64_7), unsafe.Alignof(t.c64_7))
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
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_2))))
	fmt.Println(*(*uint8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u8_3))))
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_5))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.z0_0, gT.f32_1, gT.f32_2, gT.u8_3, gT.a2_4, gT.r_5, gT.a3_6, gT.c64_7)
	fmt.Println("directO", gO.pre, gO.in.z0_0, gO.in.f32_1, gO.in.f32_2, gO.in.u8_3, gO.in.a2_4, gO.in.r_5, gO.in.a3_6, gO.in.c64_7, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{z0_0: [0]int{}, f32_1: 1.5, f32_2: 1.5, u8_3: 200, a2_4: [2]int16{-1, 2}, r_5: 'x', a3_6: [3]byte{1, 2, 3}, c64_7: 1 + 2i}
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
	loc := T{z0_0: [0]int{}, f32_1: -2.75, f32_2: -2.75, u8_3: 13, a2_4: [2]int16{300, -300}, r_5: '世', a3_6: [3]byte{7, 8, 9}, c64_7: -3 - 4i}
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
	fmt.Println(cp == gT, gT == T{z0_0: [0]int{}, f32_1: 1.5, f32_2: 1.5, u8_3: 200, a2_4: [2]int16{-1, 2}, r_5: 'x', a3_6: [3]byte{1, 2, 3}, c64_7: 1 + 2i}, dT == gT)
	cp.c64_7 = -3 - 4i
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
			res[i] = fmt.Sprint(v.z0_0, v.f32_1, v.f32_2, v.u8_3, v.a2_4, v.r_5, v.a3_6, v.c64_7, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
