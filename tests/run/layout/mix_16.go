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
	i16_1 int16
	any_2 any
	u64_3 uint64
	n2_4  N2
	i_5   int
	any_6 any
	p_7   *int
	i32_8 int32
	z1_9  struct{}
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.n1_0, x.i16_1, x.any_2, x.u64_3, x.n2_4, x.i_5, x.any_6, *x.p_7, x.i32_8, x.z1_9)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.n1_0, x.i16_1, x.any_2, x.u64_3, x.n2_4, x.i_5, x.any_6, *x.p_7, x.i32_8, x.z1_9)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.n1_0, x.i16_1, x.any_2, x.u64_3, x.n2_4, x.i_5, x.any_6, *x.p_7, x.i32_8, x.z1_9, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.n1_0, x.i16_1, x.any_2, x.u64_3, x.n2_4, x.i_5, x.any_6, *x.p_7, x.i32_8, x.z1_9, x.post)
}

var gT = T{n1_0: N1{3, -7}, i16_1: -1234, any_2: 42, u64_3: 18000000000000000000, n2_4: N2{true, [3]byte{9, 8, 7}, 2.5}, i_5: -42, any_6: 42, p_7: &ints[2], i32_8: -100000, z1_9: struct{}{}}
var gO = O{pre: 7, T: T{n1_0: N1{3, -7}, i16_1: -1234, any_2: 42, u64_3: 18000000000000000000, n2_4: N2{true, [3]byte{9, 8, 7}, 2.5}, i_5: -42, any_6: 42, p_7: &ints[2], i32_8: -100000, z1_9: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{n1_0: N1{3, -7}, i16_1: -1234, any_2: 42, u64_3: 18000000000000000000, n2_4: N2{true, [3]byte{9, 8, 7}, 2.5}, i_5: -42, any_6: 42, p_7: &ints[2], i32_8: -100000, z1_9: struct{}{}}, {}, T{n1_0: N1{-3, 70000000000}, i16_1: 4321, any_2: "dyn", u64_3: 77, n2_4: N2{false, [3]byte{1, 1, 1}, -0.5}, i_5: 123456789, any_6: "dyn", p_7: &ints[5], i32_8: 70000, z1_9: struct{}{}}}
var sT = []T{T{n1_0: N1{-3, 70000000000}, i16_1: 4321, any_2: "dyn", u64_3: 77, n2_4: N2{false, [3]byte{1, 1, 1}, -0.5}, i_5: 123456789, any_6: "dyn", p_7: &ints[5], i32_8: 70000, z1_9: struct{}{}}, T{n1_0: N1{3, -7}, i16_1: -1234, any_2: 42, u64_3: 18000000000000000000, n2_4: N2{true, [3]byte{9, 8, 7}, 2.5}, i_5: -42, any_6: 42, p_7: &ints[2], i32_8: -100000, z1_9: struct{}{}}}
var aO = [2]O{O{pre: 1, T: T{n1_0: N1{3, -7}, i16_1: -1234, any_2: 42, u64_3: 18000000000000000000, n2_4: N2{true, [3]byte{9, 8, 7}, 2.5}, i_5: -42, any_6: 42, p_7: &ints[2], i32_8: -100000, z1_9: struct{}{}}, post: 2}, O{pre: 3, T: T{n1_0: N1{-3, 70000000000}, i16_1: 4321, any_2: "dyn", u64_3: 77, n2_4: N2{false, [3]byte{1, 1, 1}, -0.5}, i_5: 123456789, any_6: "dyn", p_7: &ints[5], i32_8: 70000, z1_9: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i_5
var pFieldO = &gO.i_5
var pElem = &aT[2].i_5
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.n1_0 = N1{-3, 70000000000}
		p.i16_1 = 4321
		p.any_2 = "dyn"
		p.u64_3 = 77
		p.n2_4 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.i_5 = 123456789
		p.any_6 = "dyn"
		p.p_7 = &ints[5]
		p.i32_8 = 70000
		p.z1_9 = struct{}{}
	} else {
		p.n1_0 = N1{3, -7}
		p.i16_1 = -1234
		p.any_2 = 42
		p.u64_3 = 18000000000000000000
		p.n2_4 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.i_5 = -42
		p.any_6 = 42
		p.p_7 = &ints[2]
		p.i32_8 = -100000
		p.z1_9 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("n1_0", unsafe.Offsetof(t.n1_0), unsafe.Sizeof(t.n1_0), unsafe.Alignof(t.n1_0))
	fmt.Println("i16_1", unsafe.Offsetof(t.i16_1), unsafe.Sizeof(t.i16_1), unsafe.Alignof(t.i16_1))
	fmt.Println("any_2", unsafe.Offsetof(t.any_2), unsafe.Sizeof(t.any_2), unsafe.Alignof(t.any_2))
	fmt.Println("u64_3", unsafe.Offsetof(t.u64_3), unsafe.Sizeof(t.u64_3), unsafe.Alignof(t.u64_3))
	fmt.Println("n2_4", unsafe.Offsetof(t.n2_4), unsafe.Sizeof(t.n2_4), unsafe.Alignof(t.n2_4))
	fmt.Println("i_5", unsafe.Offsetof(t.i_5), unsafe.Sizeof(t.i_5), unsafe.Alignof(t.i_5))
	fmt.Println("any_6", unsafe.Offsetof(t.any_6), unsafe.Sizeof(t.any_6), unsafe.Alignof(t.any_6))
	fmt.Println("p_7", unsafe.Offsetof(t.p_7), unsafe.Sizeof(t.p_7), unsafe.Alignof(t.p_7))
	fmt.Println("i32_8", unsafe.Offsetof(t.i32_8), unsafe.Sizeof(t.i32_8), unsafe.Alignof(t.i32_8))
	fmt.Println("z1_9", unsafe.Offsetof(t.z1_9), unsafe.Sizeof(t.z1_9), unsafe.Alignof(t.z1_9))
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
	fmt.Println(*(*int16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i16_1))))
	fmt.Println(*(*uint64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u64_3))))
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_5))))
	fmt.Println(*(*int32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i32_8))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.n1_0, gT.i16_1, gT.any_2, gT.u64_3, gT.n2_4, gT.i_5, gT.any_6, *gT.p_7, gT.i32_8, gT.z1_9)
	fmt.Println("directO", gO.pre, gO.n1_0, gO.i16_1, gO.any_2, gO.u64_3, gO.n2_4, gO.i_5, gO.any_6, *gO.p_7, gO.i32_8, gO.z1_9, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{n1_0: N1{3, -7}, i16_1: -1234, any_2: 42, u64_3: 18000000000000000000, n2_4: N2{true, [3]byte{9, 8, 7}, 2.5}, i_5: -42, any_6: 42, p_7: &ints[2], i32_8: -100000, z1_9: struct{}{}}
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
	loc := T{n1_0: N1{-3, 70000000000}, i16_1: 4321, any_2: "dyn", u64_3: 77, n2_4: N2{false, [3]byte{1, 1, 1}, -0.5}, i_5: 123456789, any_6: "dyn", p_7: &ints[5], i32_8: 70000, z1_9: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{n1_0: N1{3, -7}, i16_1: -1234, any_2: 42, u64_3: 18000000000000000000, n2_4: N2{true, [3]byte{9, 8, 7}, 2.5}, i_5: -42, any_6: 42, p_7: &ints[2], i32_8: -100000, z1_9: struct{}{}}, dT == gT)
	cp.z1_9 = struct{}{}
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
			res[i] = fmt.Sprint(v.n1_0, v.i16_1, v.any_2, v.u64_3, v.n2_4, v.i_5, v.any_6, *v.p_7, v.i32_8, v.z1_9, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
