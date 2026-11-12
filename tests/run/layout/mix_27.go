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
	i8_0  int8
	a2_1  [2]int16
	p_2   *int
	u64_3 uint64
	c64_4 complex64
	sl_5  []int
	b_6   bool
	f64_7 float64
	a3_8  [3]byte
	z1_9  struct{}
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.i8_0, x.a2_1, *x.p_2, x.u64_3, x.c64_4, x.sl_5, x.b_6, x.f64_7, x.a3_8, x.z1_9)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.i8_0, x.a2_1, *x.p_2, x.u64_3, x.c64_4, x.sl_5, x.b_6, x.f64_7, x.a3_8, x.z1_9)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.i8_0, x.in.a2_1, *x.in.p_2, x.in.u64_3, x.in.c64_4, x.in.sl_5, x.in.b_6, x.in.f64_7, x.in.a3_8, x.in.z1_9, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.i8_0, x.in.a2_1, *x.in.p_2, x.in.u64_3, x.in.c64_4, x.in.sl_5, x.in.b_6, x.in.f64_7, x.in.a3_8, x.in.z1_9, x.post)
}

var gT = T{i8_0: -5, a2_1: [2]int16{-1, 2}, p_2: &ints[2], u64_3: 18000000000000000000, c64_4: 1 + 2i, sl_5: []int{1, 2, 3}, b_6: true, f64_7: -2.25, a3_8: [3]byte{1, 2, 3}, z1_9: struct{}{}}
var gO = O{pre: 7, in: T{i8_0: -5, a2_1: [2]int16{-1, 2}, p_2: &ints[2], u64_3: 18000000000000000000, c64_4: 1 + 2i, sl_5: []int{1, 2, 3}, b_6: true, f64_7: -2.25, a3_8: [3]byte{1, 2, 3}, z1_9: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{i8_0: -5, a2_1: [2]int16{-1, 2}, p_2: &ints[2], u64_3: 18000000000000000000, c64_4: 1 + 2i, sl_5: []int{1, 2, 3}, b_6: true, f64_7: -2.25, a3_8: [3]byte{1, 2, 3}, z1_9: struct{}{}}, {}, T{i8_0: 7, a2_1: [2]int16{300, -300}, p_2: &ints[5], u64_3: 77, c64_4: -3 - 4i, sl_5: []int{9}, b_6: false, f64_7: 1e100, a3_8: [3]byte{7, 8, 9}, z1_9: struct{}{}}}
var sT = []T{T{i8_0: 7, a2_1: [2]int16{300, -300}, p_2: &ints[5], u64_3: 77, c64_4: -3 - 4i, sl_5: []int{9}, b_6: false, f64_7: 1e100, a3_8: [3]byte{7, 8, 9}, z1_9: struct{}{}}, T{i8_0: -5, a2_1: [2]int16{-1, 2}, p_2: &ints[2], u64_3: 18000000000000000000, c64_4: 1 + 2i, sl_5: []int{1, 2, 3}, b_6: true, f64_7: -2.25, a3_8: [3]byte{1, 2, 3}, z1_9: struct{}{}}}
var aO = [2]O{O{pre: 1, in: T{i8_0: -5, a2_1: [2]int16{-1, 2}, p_2: &ints[2], u64_3: 18000000000000000000, c64_4: 1 + 2i, sl_5: []int{1, 2, 3}, b_6: true, f64_7: -2.25, a3_8: [3]byte{1, 2, 3}, z1_9: struct{}{}}, post: 2}, O{pre: 3, in: T{i8_0: 7, a2_1: [2]int16{300, -300}, p_2: &ints[5], u64_3: 77, c64_4: -3 - 4i, sl_5: []int{9}, b_6: false, f64_7: 1e100, a3_8: [3]byte{7, 8, 9}, z1_9: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.f64_7
var pFieldO = &gO.in.f64_7
var pElem = &aT[2].f64_7
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.i8_0 = 7
		p.a2_1 = [2]int16{300, -300}
		p.p_2 = &ints[5]
		p.u64_3 = 77
		p.c64_4 = -3 - 4i
		p.sl_5 = []int{9}
		p.b_6 = false
		p.f64_7 = 1e100
		p.a3_8 = [3]byte{7, 8, 9}
		p.z1_9 = struct{}{}
	} else {
		p.i8_0 = -5
		p.a2_1 = [2]int16{-1, 2}
		p.p_2 = &ints[2]
		p.u64_3 = 18000000000000000000
		p.c64_4 = 1 + 2i
		p.sl_5 = []int{1, 2, 3}
		p.b_6 = true
		p.f64_7 = -2.25
		p.a3_8 = [3]byte{1, 2, 3}
		p.z1_9 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("i8_0", unsafe.Offsetof(t.i8_0), unsafe.Sizeof(t.i8_0), unsafe.Alignof(t.i8_0))
	fmt.Println("a2_1", unsafe.Offsetof(t.a2_1), unsafe.Sizeof(t.a2_1), unsafe.Alignof(t.a2_1))
	fmt.Println("p_2", unsafe.Offsetof(t.p_2), unsafe.Sizeof(t.p_2), unsafe.Alignof(t.p_2))
	fmt.Println("u64_3", unsafe.Offsetof(t.u64_3), unsafe.Sizeof(t.u64_3), unsafe.Alignof(t.u64_3))
	fmt.Println("c64_4", unsafe.Offsetof(t.c64_4), unsafe.Sizeof(t.c64_4), unsafe.Alignof(t.c64_4))
	fmt.Println("sl_5", unsafe.Offsetof(t.sl_5), unsafe.Sizeof(t.sl_5), unsafe.Alignof(t.sl_5))
	fmt.Println("b_6", unsafe.Offsetof(t.b_6), unsafe.Sizeof(t.b_6), unsafe.Alignof(t.b_6))
	fmt.Println("f64_7", unsafe.Offsetof(t.f64_7), unsafe.Sizeof(t.f64_7), unsafe.Alignof(t.f64_7))
	fmt.Println("a3_8", unsafe.Offsetof(t.a3_8), unsafe.Sizeof(t.a3_8), unsafe.Alignof(t.a3_8))
	fmt.Println("z1_9", unsafe.Offsetof(t.z1_9), unsafe.Sizeof(t.z1_9), unsafe.Alignof(t.z1_9))
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
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_0))))
	fmt.Println(*(*uint64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u64_3))))
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_6))))
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_7))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.i8_0, gT.a2_1, *gT.p_2, gT.u64_3, gT.c64_4, gT.sl_5, gT.b_6, gT.f64_7, gT.a3_8, gT.z1_9)
	fmt.Println("directO", gO.pre, gO.in.i8_0, gO.in.a2_1, *gO.in.p_2, gO.in.u64_3, gO.in.c64_4, gO.in.sl_5, gO.in.b_6, gO.in.f64_7, gO.in.a3_8, gO.in.z1_9, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{i8_0: -5, a2_1: [2]int16{-1, 2}, p_2: &ints[2], u64_3: 18000000000000000000, c64_4: 1 + 2i, sl_5: []int{1, 2, 3}, b_6: true, f64_7: -2.25, a3_8: [3]byte{1, 2, 3}, z1_9: struct{}{}}
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
	loc := T{i8_0: 7, a2_1: [2]int16{300, -300}, p_2: &ints[5], u64_3: 77, c64_4: -3 - 4i, sl_5: []int{9}, b_6: false, f64_7: 1e100, a3_8: [3]byte{7, 8, 9}, z1_9: struct{}{}}
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
			res[i] = fmt.Sprint(v.i8_0, v.a2_1, *v.p_2, v.u64_3, v.c64_4, v.sl_5, v.b_6, v.f64_7, v.a3_8, v.z1_9, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
