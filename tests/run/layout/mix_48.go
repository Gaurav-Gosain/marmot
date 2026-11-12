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
	b_0   bool
	n2_1  N2
	n2_2  N2
	any_3 any
	n1_4  N1
	sl_5  []int
	b_6   bool
	sl_7  []int
	f32_8 float32
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
	fmt.Println(tag, x.b_0, x.n2_1, x.n2_2, x.any_3, x.n1_4, x.sl_5, x.b_6, x.sl_7, x.f32_8, x.z1_9)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.b_0, x.n2_1, x.n2_2, x.any_3, x.n1_4, x.sl_5, x.b_6, x.sl_7, x.f32_8, x.z1_9)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.b_0, x.in.n2_1, x.in.n2_2, x.in.any_3, x.in.n1_4, x.in.sl_5, x.in.b_6, x.in.sl_7, x.in.f32_8, x.in.z1_9, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.b_0, x.in.n2_1, x.in.n2_2, x.in.any_3, x.in.n1_4, x.in.sl_5, x.in.b_6, x.in.sl_7, x.in.f32_8, x.in.z1_9, x.post)
}

var gT = T{b_0: true, n2_1: N2{true, [3]byte{9, 8, 7}, 2.5}, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, any_3: 42, n1_4: N1{3, -7}, sl_5: []int{1, 2, 3}, b_6: true, sl_7: []int{1, 2, 3}, f32_8: 1.5, z1_9: struct{}{}}
var gO = O{pre: 7, in: T{b_0: true, n2_1: N2{true, [3]byte{9, 8, 7}, 2.5}, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, any_3: 42, n1_4: N1{3, -7}, sl_5: []int{1, 2, 3}, b_6: true, sl_7: []int{1, 2, 3}, f32_8: 1.5, z1_9: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{b_0: true, n2_1: N2{true, [3]byte{9, 8, 7}, 2.5}, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, any_3: 42, n1_4: N1{3, -7}, sl_5: []int{1, 2, 3}, b_6: true, sl_7: []int{1, 2, 3}, f32_8: 1.5, z1_9: struct{}{}}, {}, T{b_0: false, n2_1: N2{false, [3]byte{1, 1, 1}, -0.5}, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, any_3: "dyn", n1_4: N1{-3, 70000000000}, sl_5: []int{9}, b_6: false, sl_7: []int{9}, f32_8: -2.75, z1_9: struct{}{}}}
var sT = []T{T{b_0: false, n2_1: N2{false, [3]byte{1, 1, 1}, -0.5}, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, any_3: "dyn", n1_4: N1{-3, 70000000000}, sl_5: []int{9}, b_6: false, sl_7: []int{9}, f32_8: -2.75, z1_9: struct{}{}}, T{b_0: true, n2_1: N2{true, [3]byte{9, 8, 7}, 2.5}, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, any_3: 42, n1_4: N1{3, -7}, sl_5: []int{1, 2, 3}, b_6: true, sl_7: []int{1, 2, 3}, f32_8: 1.5, z1_9: struct{}{}}}
var aO = [2]O{O{pre: 1, in: T{b_0: true, n2_1: N2{true, [3]byte{9, 8, 7}, 2.5}, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, any_3: 42, n1_4: N1{3, -7}, sl_5: []int{1, 2, 3}, b_6: true, sl_7: []int{1, 2, 3}, f32_8: 1.5, z1_9: struct{}{}}, post: 2}, O{pre: 3, in: T{b_0: false, n2_1: N2{false, [3]byte{1, 1, 1}, -0.5}, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, any_3: "dyn", n1_4: N1{-3, 70000000000}, sl_5: []int{9}, b_6: false, sl_7: []int{9}, f32_8: -2.75, z1_9: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.b_0
var pFieldO = &gO.in.b_0
var pElem = &aT[2].b_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.b_0 = false
		p.n2_1 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.n2_2 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.any_3 = "dyn"
		p.n1_4 = N1{-3, 70000000000}
		p.sl_5 = []int{9}
		p.b_6 = false
		p.sl_7 = []int{9}
		p.f32_8 = -2.75
		p.z1_9 = struct{}{}
	} else {
		p.b_0 = true
		p.n2_1 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.n2_2 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.any_3 = 42
		p.n1_4 = N1{3, -7}
		p.sl_5 = []int{1, 2, 3}
		p.b_6 = true
		p.sl_7 = []int{1, 2, 3}
		p.f32_8 = 1.5
		p.z1_9 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("b_0", unsafe.Offsetof(t.b_0), unsafe.Sizeof(t.b_0), unsafe.Alignof(t.b_0))
	fmt.Println("n2_1", unsafe.Offsetof(t.n2_1), unsafe.Sizeof(t.n2_1), unsafe.Alignof(t.n2_1))
	fmt.Println("n2_2", unsafe.Offsetof(t.n2_2), unsafe.Sizeof(t.n2_2), unsafe.Alignof(t.n2_2))
	fmt.Println("any_3", unsafe.Offsetof(t.any_3), unsafe.Sizeof(t.any_3), unsafe.Alignof(t.any_3))
	fmt.Println("n1_4", unsafe.Offsetof(t.n1_4), unsafe.Sizeof(t.n1_4), unsafe.Alignof(t.n1_4))
	fmt.Println("sl_5", unsafe.Offsetof(t.sl_5), unsafe.Sizeof(t.sl_5), unsafe.Alignof(t.sl_5))
	fmt.Println("b_6", unsafe.Offsetof(t.b_6), unsafe.Sizeof(t.b_6), unsafe.Alignof(t.b_6))
	fmt.Println("sl_7", unsafe.Offsetof(t.sl_7), unsafe.Sizeof(t.sl_7), unsafe.Alignof(t.sl_7))
	fmt.Println("f32_8", unsafe.Offsetof(t.f32_8), unsafe.Sizeof(t.f32_8), unsafe.Alignof(t.f32_8))
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
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_0))))
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_6))))
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_8))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.b_0, gT.n2_1, gT.n2_2, gT.any_3, gT.n1_4, gT.sl_5, gT.b_6, gT.sl_7, gT.f32_8, gT.z1_9)
	fmt.Println("directO", gO.pre, gO.in.b_0, gO.in.n2_1, gO.in.n2_2, gO.in.any_3, gO.in.n1_4, gO.in.sl_5, gO.in.b_6, gO.in.sl_7, gO.in.f32_8, gO.in.z1_9, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{b_0: true, n2_1: N2{true, [3]byte{9, 8, 7}, 2.5}, n2_2: N2{true, [3]byte{9, 8, 7}, 2.5}, any_3: 42, n1_4: N1{3, -7}, sl_5: []int{1, 2, 3}, b_6: true, sl_7: []int{1, 2, 3}, f32_8: 1.5, z1_9: struct{}{}}
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
	loc := T{b_0: false, n2_1: N2{false, [3]byte{1, 1, 1}, -0.5}, n2_2: N2{false, [3]byte{1, 1, 1}, -0.5}, any_3: "dyn", n1_4: N1{-3, 70000000000}, sl_5: []int{9}, b_6: false, sl_7: []int{9}, f32_8: -2.75, z1_9: struct{}{}}
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
			res[i] = fmt.Sprint(v.b_0, v.n2_1, v.n2_2, v.any_3, v.n1_4, v.sl_5, v.b_6, v.sl_7, v.f32_8, v.z1_9, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
