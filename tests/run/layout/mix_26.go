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
	i16_0  int16
	c128_1 complex128
	u64_2  uint64
	a3_3   [3]byte
	z0_4   [0]int
	f64_5  float64
	i64_6  int64
	up_7   uintptr
	n1_8   N1
	b_9    bool
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.i16_0, x.c128_1, x.u64_2, x.a3_3, x.z0_4, x.f64_5, x.i64_6, x.up_7, x.n1_8, x.b_9)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.i16_0, x.c128_1, x.u64_2, x.a3_3, x.z0_4, x.f64_5, x.i64_6, x.up_7, x.n1_8, x.b_9)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.i16_0, x.in.c128_1, x.in.u64_2, x.in.a3_3, x.in.z0_4, x.in.f64_5, x.in.i64_6, x.in.up_7, x.in.n1_8, x.in.b_9, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.i16_0, x.in.c128_1, x.in.u64_2, x.in.a3_3, x.in.z0_4, x.in.f64_5, x.in.i64_6, x.in.up_7, x.in.n1_8, x.in.b_9, x.post)
}

var gT = T{i16_0: -1234, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, a3_3: [3]byte{1, 2, 3}, z0_4: [0]int{}, f64_5: -2.25, i64_6: -9000000000, up_7: 77, n1_8: N1{3, -7}, b_9: true}
var gO = O{pre: 7, in: T{i16_0: -1234, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, a3_3: [3]byte{1, 2, 3}, z0_4: [0]int{}, f64_5: -2.25, i64_6: -9000000000, up_7: 77, n1_8: N1{3, -7}, b_9: true}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{i16_0: -1234, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, a3_3: [3]byte{1, 2, 3}, z0_4: [0]int{}, f64_5: -2.25, i64_6: -9000000000, up_7: 77, n1_8: N1{3, -7}, b_9: true}, {}, T{i16_0: 4321, c128_1: 0.5 + 0.25i, u64_2: 77, a3_3: [3]byte{7, 8, 9}, z0_4: [0]int{}, f64_5: 1e100, i64_6: 8000000000, up_7: 88, n1_8: N1{-3, 70000000000}, b_9: false}}
var sT = []T{T{i16_0: 4321, c128_1: 0.5 + 0.25i, u64_2: 77, a3_3: [3]byte{7, 8, 9}, z0_4: [0]int{}, f64_5: 1e100, i64_6: 8000000000, up_7: 88, n1_8: N1{-3, 70000000000}, b_9: false}, T{i16_0: -1234, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, a3_3: [3]byte{1, 2, 3}, z0_4: [0]int{}, f64_5: -2.25, i64_6: -9000000000, up_7: 77, n1_8: N1{3, -7}, b_9: true}}
var aO = [2]O{O{pre: 1, in: T{i16_0: -1234, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, a3_3: [3]byte{1, 2, 3}, z0_4: [0]int{}, f64_5: -2.25, i64_6: -9000000000, up_7: 77, n1_8: N1{3, -7}, b_9: true}, post: 2}, O{pre: 3, in: T{i16_0: 4321, c128_1: 0.5 + 0.25i, u64_2: 77, a3_3: [3]byte{7, 8, 9}, z0_4: [0]int{}, f64_5: 1e100, i64_6: 8000000000, up_7: 88, n1_8: N1{-3, 70000000000}, b_9: false}, post: 4}}
var mT = map[int]T{}
var pField = &gT.f64_5
var pFieldO = &gO.in.f64_5
var pElem = &aT[2].f64_5
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.i16_0 = 4321
		p.c128_1 = 0.5 + 0.25i
		p.u64_2 = 77
		p.a3_3 = [3]byte{7, 8, 9}
		p.z0_4 = [0]int{}
		p.f64_5 = 1e100
		p.i64_6 = 8000000000
		p.up_7 = 88
		p.n1_8 = N1{-3, 70000000000}
		p.b_9 = false
	} else {
		p.i16_0 = -1234
		p.c128_1 = 3.5 - 1i
		p.u64_2 = 18000000000000000000
		p.a3_3 = [3]byte{1, 2, 3}
		p.z0_4 = [0]int{}
		p.f64_5 = -2.25
		p.i64_6 = -9000000000
		p.up_7 = 77
		p.n1_8 = N1{3, -7}
		p.b_9 = true
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("i16_0", unsafe.Offsetof(t.i16_0), unsafe.Sizeof(t.i16_0), unsafe.Alignof(t.i16_0))
	fmt.Println("c128_1", unsafe.Offsetof(t.c128_1), unsafe.Sizeof(t.c128_1), unsafe.Alignof(t.c128_1))
	fmt.Println("u64_2", unsafe.Offsetof(t.u64_2), unsafe.Sizeof(t.u64_2), unsafe.Alignof(t.u64_2))
	fmt.Println("a3_3", unsafe.Offsetof(t.a3_3), unsafe.Sizeof(t.a3_3), unsafe.Alignof(t.a3_3))
	fmt.Println("z0_4", unsafe.Offsetof(t.z0_4), unsafe.Sizeof(t.z0_4), unsafe.Alignof(t.z0_4))
	fmt.Println("f64_5", unsafe.Offsetof(t.f64_5), unsafe.Sizeof(t.f64_5), unsafe.Alignof(t.f64_5))
	fmt.Println("i64_6", unsafe.Offsetof(t.i64_6), unsafe.Sizeof(t.i64_6), unsafe.Alignof(t.i64_6))
	fmt.Println("up_7", unsafe.Offsetof(t.up_7), unsafe.Sizeof(t.up_7), unsafe.Alignof(t.up_7))
	fmt.Println("n1_8", unsafe.Offsetof(t.n1_8), unsafe.Sizeof(t.n1_8), unsafe.Alignof(t.n1_8))
	fmt.Println("b_9", unsafe.Offsetof(t.b_9), unsafe.Sizeof(t.b_9), unsafe.Alignof(t.b_9))
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
	fmt.Println(*(*int16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i16_0))))
	fmt.Println(*(*uint64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u64_2))))
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_5))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_6))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_7))))
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_9))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.i16_0, gT.c128_1, gT.u64_2, gT.a3_3, gT.z0_4, gT.f64_5, gT.i64_6, gT.up_7, gT.n1_8, gT.b_9)
	fmt.Println("directO", gO.pre, gO.in.i16_0, gO.in.c128_1, gO.in.u64_2, gO.in.a3_3, gO.in.z0_4, gO.in.f64_5, gO.in.i64_6, gO.in.up_7, gO.in.n1_8, gO.in.b_9, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{i16_0: -1234, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, a3_3: [3]byte{1, 2, 3}, z0_4: [0]int{}, f64_5: -2.25, i64_6: -9000000000, up_7: 77, n1_8: N1{3, -7}, b_9: true}
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
	loc := T{i16_0: 4321, c128_1: 0.5 + 0.25i, u64_2: 77, a3_3: [3]byte{7, 8, 9}, z0_4: [0]int{}, f64_5: 1e100, i64_6: 8000000000, up_7: 88, n1_8: N1{-3, 70000000000}, b_9: false}
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
	fmt.Println(cp == gT, gT == T{i16_0: -1234, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, a3_3: [3]byte{1, 2, 3}, z0_4: [0]int{}, f64_5: -2.25, i64_6: -9000000000, up_7: 77, n1_8: N1{3, -7}, b_9: true}, dT == gT)
	cp.b_9 = false
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
			res[i] = fmt.Sprint(v.i16_0, v.c128_1, v.u64_2, v.a3_3, v.z0_4, v.f64_5, v.i64_6, v.up_7, v.n1_8, v.b_9, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
