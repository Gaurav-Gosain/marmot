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
	i32_0 int32
	p_1   *int
	i64_2 int64
	any_3 any
	z0_4  [0]int
	i_5   int
	u16_6 uint16
	n2_7  N2
	u16_8 uint16
	a3_9  [3]byte
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.i32_0, *x.p_1, x.i64_2, x.any_3, x.z0_4, x.i_5, x.u16_6, x.n2_7, x.u16_8, x.a3_9)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.i32_0, *x.p_1, x.i64_2, x.any_3, x.z0_4, x.i_5, x.u16_6, x.n2_7, x.u16_8, x.a3_9)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.i32_0, *x.in.p_1, x.in.i64_2, x.in.any_3, x.in.z0_4, x.in.i_5, x.in.u16_6, x.in.n2_7, x.in.u16_8, x.in.a3_9, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.i32_0, *x.in.p_1, x.in.i64_2, x.in.any_3, x.in.z0_4, x.in.i_5, x.in.u16_6, x.in.n2_7, x.in.u16_8, x.in.a3_9, x.post)
}

var gT = T{i32_0: -100000, p_1: &ints[2], i64_2: -9000000000, any_3: 42, z0_4: [0]int{}, i_5: -42, u16_6: 60000, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_8: 60000, a3_9: [3]byte{1, 2, 3}}
var gO = O{pre: 7, in: T{i32_0: -100000, p_1: &ints[2], i64_2: -9000000000, any_3: 42, z0_4: [0]int{}, i_5: -42, u16_6: 60000, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_8: 60000, a3_9: [3]byte{1, 2, 3}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{i32_0: -100000, p_1: &ints[2], i64_2: -9000000000, any_3: 42, z0_4: [0]int{}, i_5: -42, u16_6: 60000, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_8: 60000, a3_9: [3]byte{1, 2, 3}}, {}, T{i32_0: 70000, p_1: &ints[5], i64_2: 8000000000, any_3: "dyn", z0_4: [0]int{}, i_5: 123456789, u16_6: 513, n2_7: N2{false, [3]byte{1, 1, 1}, -0.5}, u16_8: 513, a3_9: [3]byte{7, 8, 9}}}
var sT = []T{T{i32_0: 70000, p_1: &ints[5], i64_2: 8000000000, any_3: "dyn", z0_4: [0]int{}, i_5: 123456789, u16_6: 513, n2_7: N2{false, [3]byte{1, 1, 1}, -0.5}, u16_8: 513, a3_9: [3]byte{7, 8, 9}}, T{i32_0: -100000, p_1: &ints[2], i64_2: -9000000000, any_3: 42, z0_4: [0]int{}, i_5: -42, u16_6: 60000, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_8: 60000, a3_9: [3]byte{1, 2, 3}}}
var aO = [2]O{O{pre: 1, in: T{i32_0: -100000, p_1: &ints[2], i64_2: -9000000000, any_3: 42, z0_4: [0]int{}, i_5: -42, u16_6: 60000, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_8: 60000, a3_9: [3]byte{1, 2, 3}}, post: 2}, O{pre: 3, in: T{i32_0: 70000, p_1: &ints[5], i64_2: 8000000000, any_3: "dyn", z0_4: [0]int{}, i_5: 123456789, u16_6: 513, n2_7: N2{false, [3]byte{1, 1, 1}, -0.5}, u16_8: 513, a3_9: [3]byte{7, 8, 9}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i32_0
var pFieldO = &gO.in.i32_0
var pElem = &aT[2].i32_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.i32_0 = 70000
		p.p_1 = &ints[5]
		p.i64_2 = 8000000000
		p.any_3 = "dyn"
		p.z0_4 = [0]int{}
		p.i_5 = 123456789
		p.u16_6 = 513
		p.n2_7 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.u16_8 = 513
		p.a3_9 = [3]byte{7, 8, 9}
	} else {
		p.i32_0 = -100000
		p.p_1 = &ints[2]
		p.i64_2 = -9000000000
		p.any_3 = 42
		p.z0_4 = [0]int{}
		p.i_5 = -42
		p.u16_6 = 60000
		p.n2_7 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.u16_8 = 60000
		p.a3_9 = [3]byte{1, 2, 3}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("i32_0", unsafe.Offsetof(t.i32_0), unsafe.Sizeof(t.i32_0), unsafe.Alignof(t.i32_0))
	fmt.Println("p_1", unsafe.Offsetof(t.p_1), unsafe.Sizeof(t.p_1), unsafe.Alignof(t.p_1))
	fmt.Println("i64_2", unsafe.Offsetof(t.i64_2), unsafe.Sizeof(t.i64_2), unsafe.Alignof(t.i64_2))
	fmt.Println("any_3", unsafe.Offsetof(t.any_3), unsafe.Sizeof(t.any_3), unsafe.Alignof(t.any_3))
	fmt.Println("z0_4", unsafe.Offsetof(t.z0_4), unsafe.Sizeof(t.z0_4), unsafe.Alignof(t.z0_4))
	fmt.Println("i_5", unsafe.Offsetof(t.i_5), unsafe.Sizeof(t.i_5), unsafe.Alignof(t.i_5))
	fmt.Println("u16_6", unsafe.Offsetof(t.u16_6), unsafe.Sizeof(t.u16_6), unsafe.Alignof(t.u16_6))
	fmt.Println("n2_7", unsafe.Offsetof(t.n2_7), unsafe.Sizeof(t.n2_7), unsafe.Alignof(t.n2_7))
	fmt.Println("u16_8", unsafe.Offsetof(t.u16_8), unsafe.Sizeof(t.u16_8), unsafe.Alignof(t.u16_8))
	fmt.Println("a3_9", unsafe.Offsetof(t.a3_9), unsafe.Sizeof(t.a3_9), unsafe.Alignof(t.a3_9))
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
	fmt.Println(*(*int32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i32_0))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_2))))
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_5))))
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_6))))
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_8))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.i32_0, *gT.p_1, gT.i64_2, gT.any_3, gT.z0_4, gT.i_5, gT.u16_6, gT.n2_7, gT.u16_8, gT.a3_9)
	fmt.Println("directO", gO.pre, gO.in.i32_0, *gO.in.p_1, gO.in.i64_2, gO.in.any_3, gO.in.z0_4, gO.in.i_5, gO.in.u16_6, gO.in.n2_7, gO.in.u16_8, gO.in.a3_9, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{i32_0: -100000, p_1: &ints[2], i64_2: -9000000000, any_3: 42, z0_4: [0]int{}, i_5: -42, u16_6: 60000, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_8: 60000, a3_9: [3]byte{1, 2, 3}}
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
	loc := T{i32_0: 70000, p_1: &ints[5], i64_2: 8000000000, any_3: "dyn", z0_4: [0]int{}, i_5: 123456789, u16_6: 513, n2_7: N2{false, [3]byte{1, 1, 1}, -0.5}, u16_8: 513, a3_9: [3]byte{7, 8, 9}}
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
	fmt.Println(cp == gT, gT == T{i32_0: -100000, p_1: &ints[2], i64_2: -9000000000, any_3: 42, z0_4: [0]int{}, i_5: -42, u16_6: 60000, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_8: 60000, a3_9: [3]byte{1, 2, 3}}, dT == gT)
	cp.a3_9 = [3]byte{7, 8, 9}
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
			res[i] = fmt.Sprint(v.i32_0, *v.p_1, v.i64_2, v.any_3, v.z0_4, v.i_5, v.u16_6, v.n2_7, v.u16_8, v.a3_9, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
