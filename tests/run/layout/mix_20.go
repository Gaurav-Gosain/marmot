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
	n2_0  N2
	u64_1 uint64
	c64_2 complex64
	i64_3 int64
	i8_4  int8
	c64_5 complex64
	z1_6  struct{}
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.n2_0, x.u64_1, x.c64_2, x.i64_3, x.i8_4, x.c64_5, x.z1_6)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.n2_0, x.u64_1, x.c64_2, x.i64_3, x.i8_4, x.c64_5, x.z1_6)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.n2_0, x.in.u64_1, x.in.c64_2, x.in.i64_3, x.in.i8_4, x.in.c64_5, x.in.z1_6, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.n2_0, x.in.u64_1, x.in.c64_2, x.in.i64_3, x.in.i8_4, x.in.c64_5, x.in.z1_6, x.post)
}

var gT = T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_1: 18000000000000000000, c64_2: 1 + 2i, i64_3: -9000000000, i8_4: -5, c64_5: 1 + 2i, z1_6: struct{}{}}
var gO = O{pre: 7, in: T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_1: 18000000000000000000, c64_2: 1 + 2i, i64_3: -9000000000, i8_4: -5, c64_5: 1 + 2i, z1_6: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_1: 18000000000000000000, c64_2: 1 + 2i, i64_3: -9000000000, i8_4: -5, c64_5: 1 + 2i, z1_6: struct{}{}}, {}, T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, u64_1: 77, c64_2: -3 - 4i, i64_3: 8000000000, i8_4: 7, c64_5: -3 - 4i, z1_6: struct{}{}}}
var sT = []T{T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, u64_1: 77, c64_2: -3 - 4i, i64_3: 8000000000, i8_4: 7, c64_5: -3 - 4i, z1_6: struct{}{}}, T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_1: 18000000000000000000, c64_2: 1 + 2i, i64_3: -9000000000, i8_4: -5, c64_5: 1 + 2i, z1_6: struct{}{}}}
var aO = [2]O{O{pre: 1, in: T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_1: 18000000000000000000, c64_2: 1 + 2i, i64_3: -9000000000, i8_4: -5, c64_5: 1 + 2i, z1_6: struct{}{}}, post: 2}, O{pre: 3, in: T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, u64_1: 77, c64_2: -3 - 4i, i64_3: 8000000000, i8_4: 7, c64_5: -3 - 4i, z1_6: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i64_3
var pFieldO = &gO.in.i64_3
var pElem = &aT[2].i64_3
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.n2_0 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.u64_1 = 77
		p.c64_2 = -3 - 4i
		p.i64_3 = 8000000000
		p.i8_4 = 7
		p.c64_5 = -3 - 4i
		p.z1_6 = struct{}{}
	} else {
		p.n2_0 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.u64_1 = 18000000000000000000
		p.c64_2 = 1 + 2i
		p.i64_3 = -9000000000
		p.i8_4 = -5
		p.c64_5 = 1 + 2i
		p.z1_6 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("n2_0", unsafe.Offsetof(t.n2_0), unsafe.Sizeof(t.n2_0), unsafe.Alignof(t.n2_0))
	fmt.Println("u64_1", unsafe.Offsetof(t.u64_1), unsafe.Sizeof(t.u64_1), unsafe.Alignof(t.u64_1))
	fmt.Println("c64_2", unsafe.Offsetof(t.c64_2), unsafe.Sizeof(t.c64_2), unsafe.Alignof(t.c64_2))
	fmt.Println("i64_3", unsafe.Offsetof(t.i64_3), unsafe.Sizeof(t.i64_3), unsafe.Alignof(t.i64_3))
	fmt.Println("i8_4", unsafe.Offsetof(t.i8_4), unsafe.Sizeof(t.i8_4), unsafe.Alignof(t.i8_4))
	fmt.Println("c64_5", unsafe.Offsetof(t.c64_5), unsafe.Sizeof(t.c64_5), unsafe.Alignof(t.c64_5))
	fmt.Println("z1_6", unsafe.Offsetof(t.z1_6), unsafe.Sizeof(t.z1_6), unsafe.Alignof(t.z1_6))
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
	fmt.Println(*(*uint64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u64_1))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_3))))
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_4))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.n2_0, gT.u64_1, gT.c64_2, gT.i64_3, gT.i8_4, gT.c64_5, gT.z1_6)
	fmt.Println("directO", gO.pre, gO.in.n2_0, gO.in.u64_1, gO.in.c64_2, gO.in.i64_3, gO.in.i8_4, gO.in.c64_5, gO.in.z1_6, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_1: 18000000000000000000, c64_2: 1 + 2i, i64_3: -9000000000, i8_4: -5, c64_5: 1 + 2i, z1_6: struct{}{}}
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
	loc := T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, u64_1: 77, c64_2: -3 - 4i, i64_3: 8000000000, i8_4: 7, c64_5: -3 - 4i, z1_6: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, u64_1: 18000000000000000000, c64_2: 1 + 2i, i64_3: -9000000000, i8_4: -5, c64_5: 1 + 2i, z1_6: struct{}{}}, dT == gT)
	cp.z1_6 = struct{}{}
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
			res[i] = fmt.Sprint(v.n2_0, v.u64_1, v.c64_2, v.i64_3, v.i8_4, v.c64_5, v.z1_6, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
