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
	n1_0  N1
	a2_1  [2]int16
	i8_2  int8
	r_3   rune
	z0_4  [0]int
	i16_5 int16
	u16_6 uint16
	u32_7 uint32
	f32_8 float32
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.n1_0, x.a2_1, x.i8_2, x.r_3, x.z0_4, x.i16_5, x.u16_6, x.u32_7, x.f32_8)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.n1_0, x.a2_1, x.i8_2, x.r_3, x.z0_4, x.i16_5, x.u16_6, x.u32_7, x.f32_8)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.n1_0, x.a2_1, x.i8_2, x.r_3, x.z0_4, x.i16_5, x.u16_6, x.u32_7, x.f32_8, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.n1_0, x.a2_1, x.i8_2, x.r_3, x.z0_4, x.i16_5, x.u16_6, x.u32_7, x.f32_8, x.post)
}

var gT = T{n1_0: N1{3, -7}, a2_1: [2]int16{-1, 2}, i8_2: -5, r_3: 'x', z0_4: [0]int{}, i16_5: -1234, u16_6: 60000, u32_7: 4000000000, f32_8: 1.5}
var gO = O{pre: 7, T: T{n1_0: N1{3, -7}, a2_1: [2]int16{-1, 2}, i8_2: -5, r_3: 'x', z0_4: [0]int{}, i16_5: -1234, u16_6: 60000, u32_7: 4000000000, f32_8: 1.5}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{n1_0: N1{3, -7}, a2_1: [2]int16{-1, 2}, i8_2: -5, r_3: 'x', z0_4: [0]int{}, i16_5: -1234, u16_6: 60000, u32_7: 4000000000, f32_8: 1.5}, {}, T{n1_0: N1{-3, 70000000000}, a2_1: [2]int16{300, -300}, i8_2: 7, r_3: '世', z0_4: [0]int{}, i16_5: 4321, u16_6: 513, u32_7: 99, f32_8: -2.75}}
var sT = []T{T{n1_0: N1{-3, 70000000000}, a2_1: [2]int16{300, -300}, i8_2: 7, r_3: '世', z0_4: [0]int{}, i16_5: 4321, u16_6: 513, u32_7: 99, f32_8: -2.75}, T{n1_0: N1{3, -7}, a2_1: [2]int16{-1, 2}, i8_2: -5, r_3: 'x', z0_4: [0]int{}, i16_5: -1234, u16_6: 60000, u32_7: 4000000000, f32_8: 1.5}}
var aO = [2]O{O{pre: 1, T: T{n1_0: N1{3, -7}, a2_1: [2]int16{-1, 2}, i8_2: -5, r_3: 'x', z0_4: [0]int{}, i16_5: -1234, u16_6: 60000, u32_7: 4000000000, f32_8: 1.5}, post: 2}, O{pre: 3, T: T{n1_0: N1{-3, 70000000000}, a2_1: [2]int16{300, -300}, i8_2: 7, r_3: '世', z0_4: [0]int{}, i16_5: 4321, u16_6: 513, u32_7: 99, f32_8: -2.75}, post: 4}}
var mT = map[int]T{}
var pField = &gT.n1_0
var pFieldO = &gO.n1_0
var pElem = &aT[2].n1_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.n1_0 = N1{-3, 70000000000}
		p.a2_1 = [2]int16{300, -300}
		p.i8_2 = 7
		p.r_3 = '世'
		p.z0_4 = [0]int{}
		p.i16_5 = 4321
		p.u16_6 = 513
		p.u32_7 = 99
		p.f32_8 = -2.75
	} else {
		p.n1_0 = N1{3, -7}
		p.a2_1 = [2]int16{-1, 2}
		p.i8_2 = -5
		p.r_3 = 'x'
		p.z0_4 = [0]int{}
		p.i16_5 = -1234
		p.u16_6 = 60000
		p.u32_7 = 4000000000
		p.f32_8 = 1.5
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("n1_0", unsafe.Offsetof(t.n1_0), unsafe.Sizeof(t.n1_0), unsafe.Alignof(t.n1_0))
	fmt.Println("a2_1", unsafe.Offsetof(t.a2_1), unsafe.Sizeof(t.a2_1), unsafe.Alignof(t.a2_1))
	fmt.Println("i8_2", unsafe.Offsetof(t.i8_2), unsafe.Sizeof(t.i8_2), unsafe.Alignof(t.i8_2))
	fmt.Println("r_3", unsafe.Offsetof(t.r_3), unsafe.Sizeof(t.r_3), unsafe.Alignof(t.r_3))
	fmt.Println("z0_4", unsafe.Offsetof(t.z0_4), unsafe.Sizeof(t.z0_4), unsafe.Alignof(t.z0_4))
	fmt.Println("i16_5", unsafe.Offsetof(t.i16_5), unsafe.Sizeof(t.i16_5), unsafe.Alignof(t.i16_5))
	fmt.Println("u16_6", unsafe.Offsetof(t.u16_6), unsafe.Sizeof(t.u16_6), unsafe.Alignof(t.u16_6))
	fmt.Println("u32_7", unsafe.Offsetof(t.u32_7), unsafe.Sizeof(t.u32_7), unsafe.Alignof(t.u32_7))
	fmt.Println("f32_8", unsafe.Offsetof(t.f32_8), unsafe.Sizeof(t.f32_8), unsafe.Alignof(t.f32_8))
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
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_2))))
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_3))))
	fmt.Println(*(*int16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i16_5))))
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_6))))
	fmt.Println(*(*uint32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u32_7))))
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_8))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.n1_0, gT.a2_1, gT.i8_2, gT.r_3, gT.z0_4, gT.i16_5, gT.u16_6, gT.u32_7, gT.f32_8)
	fmt.Println("directO", gO.pre, gO.n1_0, gO.a2_1, gO.i8_2, gO.r_3, gO.z0_4, gO.i16_5, gO.u16_6, gO.u32_7, gO.f32_8, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{n1_0: N1{3, -7}, a2_1: [2]int16{-1, 2}, i8_2: -5, r_3: 'x', z0_4: [0]int{}, i16_5: -1234, u16_6: 60000, u32_7: 4000000000, f32_8: 1.5}
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
	loc := T{n1_0: N1{-3, 70000000000}, a2_1: [2]int16{300, -300}, i8_2: 7, r_3: '世', z0_4: [0]int{}, i16_5: 4321, u16_6: 513, u32_7: 99, f32_8: -2.75}
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
	fmt.Println(cp == gT, gT == T{n1_0: N1{3, -7}, a2_1: [2]int16{-1, 2}, i8_2: -5, r_3: 'x', z0_4: [0]int{}, i16_5: -1234, u16_6: 60000, u32_7: 4000000000, f32_8: 1.5}, dT == gT)
	cp.f32_8 = -2.75
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
			res[i] = fmt.Sprint(v.n1_0, v.a2_1, v.i8_2, v.r_3, v.z0_4, v.i16_5, v.u16_6, v.u32_7, v.f32_8, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
