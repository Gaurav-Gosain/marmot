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
	u32_0 uint32
	b_1   bool
	n1_2  N1
	up_3  uintptr
	u16_4 uint16
	i32_5 int32
	r_6   rune
	z1_7  struct{}
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.u32_0, x.b_1, x.n1_2, x.up_3, x.u16_4, x.i32_5, x.r_6, x.z1_7)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.u32_0, x.b_1, x.n1_2, x.up_3, x.u16_4, x.i32_5, x.r_6, x.z1_7)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.u32_0, x.b_1, x.n1_2, x.up_3, x.u16_4, x.i32_5, x.r_6, x.z1_7, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.u32_0, x.b_1, x.n1_2, x.up_3, x.u16_4, x.i32_5, x.r_6, x.z1_7, x.post)
}

var gT = T{u32_0: 4000000000, b_1: true, n1_2: N1{3, -7}, up_3: 77, u16_4: 60000, i32_5: -100000, r_6: 'x', z1_7: struct{}{}}
var gO = O{pre: 7, T: T{u32_0: 4000000000, b_1: true, n1_2: N1{3, -7}, up_3: 77, u16_4: 60000, i32_5: -100000, r_6: 'x', z1_7: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{u32_0: 4000000000, b_1: true, n1_2: N1{3, -7}, up_3: 77, u16_4: 60000, i32_5: -100000, r_6: 'x', z1_7: struct{}{}}, {}, T{u32_0: 99, b_1: false, n1_2: N1{-3, 70000000000}, up_3: 88, u16_4: 513, i32_5: 70000, r_6: '世', z1_7: struct{}{}}}
var sT = []T{T{u32_0: 99, b_1: false, n1_2: N1{-3, 70000000000}, up_3: 88, u16_4: 513, i32_5: 70000, r_6: '世', z1_7: struct{}{}}, T{u32_0: 4000000000, b_1: true, n1_2: N1{3, -7}, up_3: 77, u16_4: 60000, i32_5: -100000, r_6: 'x', z1_7: struct{}{}}}
var aO = [2]O{O{pre: 1, T: T{u32_0: 4000000000, b_1: true, n1_2: N1{3, -7}, up_3: 77, u16_4: 60000, i32_5: -100000, r_6: 'x', z1_7: struct{}{}}, post: 2}, O{pre: 3, T: T{u32_0: 99, b_1: false, n1_2: N1{-3, 70000000000}, up_3: 88, u16_4: 513, i32_5: 70000, r_6: '世', z1_7: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i32_5
var pFieldO = &gO.i32_5
var pElem = &aT[2].i32_5
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.u32_0 = 99
		p.b_1 = false
		p.n1_2 = N1{-3, 70000000000}
		p.up_3 = 88
		p.u16_4 = 513
		p.i32_5 = 70000
		p.r_6 = '世'
		p.z1_7 = struct{}{}
	} else {
		p.u32_0 = 4000000000
		p.b_1 = true
		p.n1_2 = N1{3, -7}
		p.up_3 = 77
		p.u16_4 = 60000
		p.i32_5 = -100000
		p.r_6 = 'x'
		p.z1_7 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("u32_0", unsafe.Offsetof(t.u32_0), unsafe.Sizeof(t.u32_0), unsafe.Alignof(t.u32_0))
	fmt.Println("b_1", unsafe.Offsetof(t.b_1), unsafe.Sizeof(t.b_1), unsafe.Alignof(t.b_1))
	fmt.Println("n1_2", unsafe.Offsetof(t.n1_2), unsafe.Sizeof(t.n1_2), unsafe.Alignof(t.n1_2))
	fmt.Println("up_3", unsafe.Offsetof(t.up_3), unsafe.Sizeof(t.up_3), unsafe.Alignof(t.up_3))
	fmt.Println("u16_4", unsafe.Offsetof(t.u16_4), unsafe.Sizeof(t.u16_4), unsafe.Alignof(t.u16_4))
	fmt.Println("i32_5", unsafe.Offsetof(t.i32_5), unsafe.Sizeof(t.i32_5), unsafe.Alignof(t.i32_5))
	fmt.Println("r_6", unsafe.Offsetof(t.r_6), unsafe.Sizeof(t.r_6), unsafe.Alignof(t.r_6))
	fmt.Println("z1_7", unsafe.Offsetof(t.z1_7), unsafe.Sizeof(t.z1_7), unsafe.Alignof(t.z1_7))
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
	fmt.Println(*(*uint32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u32_0))))
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_1))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_3))))
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_4))))
	fmt.Println(*(*int32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i32_5))))
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_6))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.u32_0, gT.b_1, gT.n1_2, gT.up_3, gT.u16_4, gT.i32_5, gT.r_6, gT.z1_7)
	fmt.Println("directO", gO.pre, gO.u32_0, gO.b_1, gO.n1_2, gO.up_3, gO.u16_4, gO.i32_5, gO.r_6, gO.z1_7, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{u32_0: 4000000000, b_1: true, n1_2: N1{3, -7}, up_3: 77, u16_4: 60000, i32_5: -100000, r_6: 'x', z1_7: struct{}{}}
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
	loc := T{u32_0: 99, b_1: false, n1_2: N1{-3, 70000000000}, up_3: 88, u16_4: 513, i32_5: 70000, r_6: '世', z1_7: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{u32_0: 4000000000, b_1: true, n1_2: N1{3, -7}, up_3: 77, u16_4: 60000, i32_5: -100000, r_6: 'x', z1_7: struct{}{}}, dT == gT)
	cp.z1_7 = struct{}{}
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
			res[i] = fmt.Sprint(v.u32_0, v.b_1, v.n1_2, v.up_3, v.u16_4, v.i32_5, v.r_6, v.z1_7, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
