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
	u16_0 uint16
	u64_1 uint64
	f64_2 float64
	n2_3  N2
	up_4  uintptr
	i8_5  int8
	a3_6  [3]byte
	any_7 any
	z1_8  struct{}
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.u16_0, x.u64_1, x.f64_2, x.n2_3, x.up_4, x.i8_5, x.a3_6, x.any_7, x.z1_8)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.u16_0, x.u64_1, x.f64_2, x.n2_3, x.up_4, x.i8_5, x.a3_6, x.any_7, x.z1_8)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.u16_0, x.in.u64_1, x.in.f64_2, x.in.n2_3, x.in.up_4, x.in.i8_5, x.in.a3_6, x.in.any_7, x.in.z1_8, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.u16_0, x.in.u64_1, x.in.f64_2, x.in.n2_3, x.in.up_4, x.in.i8_5, x.in.a3_6, x.in.any_7, x.in.z1_8, x.post)
}

var gT = T{u16_0: 60000, u64_1: 18000000000000000000, f64_2: -2.25, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, up_4: 77, i8_5: -5, a3_6: [3]byte{1, 2, 3}, any_7: 42, z1_8: struct{}{}}
var gO = O{pre: 7, in: T{u16_0: 60000, u64_1: 18000000000000000000, f64_2: -2.25, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, up_4: 77, i8_5: -5, a3_6: [3]byte{1, 2, 3}, any_7: 42, z1_8: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{u16_0: 60000, u64_1: 18000000000000000000, f64_2: -2.25, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, up_4: 77, i8_5: -5, a3_6: [3]byte{1, 2, 3}, any_7: 42, z1_8: struct{}{}}, {}, T{u16_0: 513, u64_1: 77, f64_2: 1e100, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, up_4: 88, i8_5: 7, a3_6: [3]byte{7, 8, 9}, any_7: "dyn", z1_8: struct{}{}}}
var sT = []T{T{u16_0: 513, u64_1: 77, f64_2: 1e100, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, up_4: 88, i8_5: 7, a3_6: [3]byte{7, 8, 9}, any_7: "dyn", z1_8: struct{}{}}, T{u16_0: 60000, u64_1: 18000000000000000000, f64_2: -2.25, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, up_4: 77, i8_5: -5, a3_6: [3]byte{1, 2, 3}, any_7: 42, z1_8: struct{}{}}}
var aO = [2]O{O{pre: 1, in: T{u16_0: 60000, u64_1: 18000000000000000000, f64_2: -2.25, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, up_4: 77, i8_5: -5, a3_6: [3]byte{1, 2, 3}, any_7: 42, z1_8: struct{}{}}, post: 2}, O{pre: 3, in: T{u16_0: 513, u64_1: 77, f64_2: 1e100, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, up_4: 88, i8_5: 7, a3_6: [3]byte{7, 8, 9}, any_7: "dyn", z1_8: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.f64_2
var pFieldO = &gO.in.f64_2
var pElem = &aT[2].f64_2
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.u16_0 = 513
		p.u64_1 = 77
		p.f64_2 = 1e100
		p.n2_3 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.up_4 = 88
		p.i8_5 = 7
		p.a3_6 = [3]byte{7, 8, 9}
		p.any_7 = "dyn"
		p.z1_8 = struct{}{}
	} else {
		p.u16_0 = 60000
		p.u64_1 = 18000000000000000000
		p.f64_2 = -2.25
		p.n2_3 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.up_4 = 77
		p.i8_5 = -5
		p.a3_6 = [3]byte{1, 2, 3}
		p.any_7 = 42
		p.z1_8 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("u16_0", unsafe.Offsetof(t.u16_0), unsafe.Sizeof(t.u16_0), unsafe.Alignof(t.u16_0))
	fmt.Println("u64_1", unsafe.Offsetof(t.u64_1), unsafe.Sizeof(t.u64_1), unsafe.Alignof(t.u64_1))
	fmt.Println("f64_2", unsafe.Offsetof(t.f64_2), unsafe.Sizeof(t.f64_2), unsafe.Alignof(t.f64_2))
	fmt.Println("n2_3", unsafe.Offsetof(t.n2_3), unsafe.Sizeof(t.n2_3), unsafe.Alignof(t.n2_3))
	fmt.Println("up_4", unsafe.Offsetof(t.up_4), unsafe.Sizeof(t.up_4), unsafe.Alignof(t.up_4))
	fmt.Println("i8_5", unsafe.Offsetof(t.i8_5), unsafe.Sizeof(t.i8_5), unsafe.Alignof(t.i8_5))
	fmt.Println("a3_6", unsafe.Offsetof(t.a3_6), unsafe.Sizeof(t.a3_6), unsafe.Alignof(t.a3_6))
	fmt.Println("any_7", unsafe.Offsetof(t.any_7), unsafe.Sizeof(t.any_7), unsafe.Alignof(t.any_7))
	fmt.Println("z1_8", unsafe.Offsetof(t.z1_8), unsafe.Sizeof(t.z1_8), unsafe.Alignof(t.z1_8))
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
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_0))))
	fmt.Println(*(*uint64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u64_1))))
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_2))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_4))))
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_5))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.u16_0, gT.u64_1, gT.f64_2, gT.n2_3, gT.up_4, gT.i8_5, gT.a3_6, gT.any_7, gT.z1_8)
	fmt.Println("directO", gO.pre, gO.in.u16_0, gO.in.u64_1, gO.in.f64_2, gO.in.n2_3, gO.in.up_4, gO.in.i8_5, gO.in.a3_6, gO.in.any_7, gO.in.z1_8, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{u16_0: 60000, u64_1: 18000000000000000000, f64_2: -2.25, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, up_4: 77, i8_5: -5, a3_6: [3]byte{1, 2, 3}, any_7: 42, z1_8: struct{}{}}
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
	loc := T{u16_0: 513, u64_1: 77, f64_2: 1e100, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, up_4: 88, i8_5: 7, a3_6: [3]byte{7, 8, 9}, any_7: "dyn", z1_8: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{u16_0: 60000, u64_1: 18000000000000000000, f64_2: -2.25, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, up_4: 77, i8_5: -5, a3_6: [3]byte{1, 2, 3}, any_7: 42, z1_8: struct{}{}}, dT == gT)
	cp.z1_8 = struct{}{}
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
			res[i] = fmt.Sprint(v.u16_0, v.u64_1, v.f64_2, v.n2_3, v.up_4, v.i8_5, v.a3_6, v.any_7, v.z1_8, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
