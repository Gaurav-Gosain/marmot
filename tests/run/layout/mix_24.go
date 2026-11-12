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
	f64_0 float64
	p_1   *int
	i_2   int
	sl_3  []int
	i32_4 int32
	any_5 any
	f32_6 float32
	f64_7 float64
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
	fmt.Println(tag, x.f64_0, *x.p_1, x.i_2, x.sl_3, x.i32_4, x.any_5, x.f32_6, x.f64_7, x.z1_8)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.f64_0, *x.p_1, x.i_2, x.sl_3, x.i32_4, x.any_5, x.f32_6, x.f64_7, x.z1_8)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.f64_0, *x.in.p_1, x.in.i_2, x.in.sl_3, x.in.i32_4, x.in.any_5, x.in.f32_6, x.in.f64_7, x.in.z1_8, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.f64_0, *x.in.p_1, x.in.i_2, x.in.sl_3, x.in.i32_4, x.in.any_5, x.in.f32_6, x.in.f64_7, x.in.z1_8, x.post)
}

var gT = T{f64_0: -2.25, p_1: &ints[2], i_2: -42, sl_3: []int{1, 2, 3}, i32_4: -100000, any_5: 42, f32_6: 1.5, f64_7: -2.25, z1_8: struct{}{}}
var gO = O{pre: 7, in: T{f64_0: -2.25, p_1: &ints[2], i_2: -42, sl_3: []int{1, 2, 3}, i32_4: -100000, any_5: 42, f32_6: 1.5, f64_7: -2.25, z1_8: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{f64_0: -2.25, p_1: &ints[2], i_2: -42, sl_3: []int{1, 2, 3}, i32_4: -100000, any_5: 42, f32_6: 1.5, f64_7: -2.25, z1_8: struct{}{}}, {}, T{f64_0: 1e100, p_1: &ints[5], i_2: 123456789, sl_3: []int{9}, i32_4: 70000, any_5: "dyn", f32_6: -2.75, f64_7: 1e100, z1_8: struct{}{}}}
var sT = []T{T{f64_0: 1e100, p_1: &ints[5], i_2: 123456789, sl_3: []int{9}, i32_4: 70000, any_5: "dyn", f32_6: -2.75, f64_7: 1e100, z1_8: struct{}{}}, T{f64_0: -2.25, p_1: &ints[2], i_2: -42, sl_3: []int{1, 2, 3}, i32_4: -100000, any_5: 42, f32_6: 1.5, f64_7: -2.25, z1_8: struct{}{}}}
var aO = [2]O{O{pre: 1, in: T{f64_0: -2.25, p_1: &ints[2], i_2: -42, sl_3: []int{1, 2, 3}, i32_4: -100000, any_5: 42, f32_6: 1.5, f64_7: -2.25, z1_8: struct{}{}}, post: 2}, O{pre: 3, in: T{f64_0: 1e100, p_1: &ints[5], i_2: 123456789, sl_3: []int{9}, i32_4: 70000, any_5: "dyn", f32_6: -2.75, f64_7: 1e100, z1_8: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.f64_0
var pFieldO = &gO.in.f64_0
var pElem = &aT[2].f64_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.f64_0 = 1e100
		p.p_1 = &ints[5]
		p.i_2 = 123456789
		p.sl_3 = []int{9}
		p.i32_4 = 70000
		p.any_5 = "dyn"
		p.f32_6 = -2.75
		p.f64_7 = 1e100
		p.z1_8 = struct{}{}
	} else {
		p.f64_0 = -2.25
		p.p_1 = &ints[2]
		p.i_2 = -42
		p.sl_3 = []int{1, 2, 3}
		p.i32_4 = -100000
		p.any_5 = 42
		p.f32_6 = 1.5
		p.f64_7 = -2.25
		p.z1_8 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("f64_0", unsafe.Offsetof(t.f64_0), unsafe.Sizeof(t.f64_0), unsafe.Alignof(t.f64_0))
	fmt.Println("p_1", unsafe.Offsetof(t.p_1), unsafe.Sizeof(t.p_1), unsafe.Alignof(t.p_1))
	fmt.Println("i_2", unsafe.Offsetof(t.i_2), unsafe.Sizeof(t.i_2), unsafe.Alignof(t.i_2))
	fmt.Println("sl_3", unsafe.Offsetof(t.sl_3), unsafe.Sizeof(t.sl_3), unsafe.Alignof(t.sl_3))
	fmt.Println("i32_4", unsafe.Offsetof(t.i32_4), unsafe.Sizeof(t.i32_4), unsafe.Alignof(t.i32_4))
	fmt.Println("any_5", unsafe.Offsetof(t.any_5), unsafe.Sizeof(t.any_5), unsafe.Alignof(t.any_5))
	fmt.Println("f32_6", unsafe.Offsetof(t.f32_6), unsafe.Sizeof(t.f32_6), unsafe.Alignof(t.f32_6))
	fmt.Println("f64_7", unsafe.Offsetof(t.f64_7), unsafe.Sizeof(t.f64_7), unsafe.Alignof(t.f64_7))
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
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_0))))
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_2))))
	fmt.Println(*(*int32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i32_4))))
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_6))))
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_7))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.f64_0, *gT.p_1, gT.i_2, gT.sl_3, gT.i32_4, gT.any_5, gT.f32_6, gT.f64_7, gT.z1_8)
	fmt.Println("directO", gO.pre, gO.in.f64_0, *gO.in.p_1, gO.in.i_2, gO.in.sl_3, gO.in.i32_4, gO.in.any_5, gO.in.f32_6, gO.in.f64_7, gO.in.z1_8, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{f64_0: -2.25, p_1: &ints[2], i_2: -42, sl_3: []int{1, 2, 3}, i32_4: -100000, any_5: 42, f32_6: 1.5, f64_7: -2.25, z1_8: struct{}{}}
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
	loc := T{f64_0: 1e100, p_1: &ints[5], i_2: 123456789, sl_3: []int{9}, i32_4: 70000, any_5: "dyn", f32_6: -2.75, f64_7: 1e100, z1_8: struct{}{}}
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
			res[i] = fmt.Sprint(v.f64_0, *v.p_1, v.i_2, v.sl_3, v.i32_4, v.any_5, v.f32_6, v.f64_7, v.z1_8, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
