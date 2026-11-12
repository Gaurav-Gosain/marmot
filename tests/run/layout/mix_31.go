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
	up_0   uintptr
	c128_1 complex128
	u64_2  uint64
	n2_3   N2
	b_4    bool
	c64_5  complex64
	n2_6   N2
	u16_7  uint16
	i_8    int
	z1_9   struct{}
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.up_0, x.c128_1, x.u64_2, x.n2_3, x.b_4, x.c64_5, x.n2_6, x.u16_7, x.i_8, x.z1_9)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.up_0, x.c128_1, x.u64_2, x.n2_3, x.b_4, x.c64_5, x.n2_6, x.u16_7, x.i_8, x.z1_9)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.up_0, x.c128_1, x.u64_2, x.n2_3, x.b_4, x.c64_5, x.n2_6, x.u16_7, x.i_8, x.z1_9, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.up_0, x.c128_1, x.u64_2, x.n2_3, x.b_4, x.c64_5, x.n2_6, x.u16_7, x.i_8, x.z1_9, x.post)
}

var gT = T{up_0: 77, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, b_4: true, c64_5: 1 + 2i, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_7: 60000, i_8: -42, z1_9: struct{}{}}
var gO = O{pre: 7, T: T{up_0: 77, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, b_4: true, c64_5: 1 + 2i, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_7: 60000, i_8: -42, z1_9: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{up_0: 77, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, b_4: true, c64_5: 1 + 2i, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_7: 60000, i_8: -42, z1_9: struct{}{}}, {}, T{up_0: 88, c128_1: 0.5 + 0.25i, u64_2: 77, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, b_4: false, c64_5: -3 - 4i, n2_6: N2{false, [3]byte{1, 1, 1}, -0.5}, u16_7: 513, i_8: 123456789, z1_9: struct{}{}}}
var sT = []T{T{up_0: 88, c128_1: 0.5 + 0.25i, u64_2: 77, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, b_4: false, c64_5: -3 - 4i, n2_6: N2{false, [3]byte{1, 1, 1}, -0.5}, u16_7: 513, i_8: 123456789, z1_9: struct{}{}}, T{up_0: 77, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, b_4: true, c64_5: 1 + 2i, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_7: 60000, i_8: -42, z1_9: struct{}{}}}
var aO = [2]O{O{pre: 1, T: T{up_0: 77, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, b_4: true, c64_5: 1 + 2i, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_7: 60000, i_8: -42, z1_9: struct{}{}}, post: 2}, O{pre: 3, T: T{up_0: 88, c128_1: 0.5 + 0.25i, u64_2: 77, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, b_4: false, c64_5: -3 - 4i, n2_6: N2{false, [3]byte{1, 1, 1}, -0.5}, u16_7: 513, i_8: 123456789, z1_9: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i_8
var pFieldO = &gO.i_8
var pElem = &aT[2].i_8
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.up_0 = 88
		p.c128_1 = 0.5 + 0.25i
		p.u64_2 = 77
		p.n2_3 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.b_4 = false
		p.c64_5 = -3 - 4i
		p.n2_6 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.u16_7 = 513
		p.i_8 = 123456789
		p.z1_9 = struct{}{}
	} else {
		p.up_0 = 77
		p.c128_1 = 3.5 - 1i
		p.u64_2 = 18000000000000000000
		p.n2_3 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.b_4 = true
		p.c64_5 = 1 + 2i
		p.n2_6 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.u16_7 = 60000
		p.i_8 = -42
		p.z1_9 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("up_0", unsafe.Offsetof(t.up_0), unsafe.Sizeof(t.up_0), unsafe.Alignof(t.up_0))
	fmt.Println("c128_1", unsafe.Offsetof(t.c128_1), unsafe.Sizeof(t.c128_1), unsafe.Alignof(t.c128_1))
	fmt.Println("u64_2", unsafe.Offsetof(t.u64_2), unsafe.Sizeof(t.u64_2), unsafe.Alignof(t.u64_2))
	fmt.Println("n2_3", unsafe.Offsetof(t.n2_3), unsafe.Sizeof(t.n2_3), unsafe.Alignof(t.n2_3))
	fmt.Println("b_4", unsafe.Offsetof(t.b_4), unsafe.Sizeof(t.b_4), unsafe.Alignof(t.b_4))
	fmt.Println("c64_5", unsafe.Offsetof(t.c64_5), unsafe.Sizeof(t.c64_5), unsafe.Alignof(t.c64_5))
	fmt.Println("n2_6", unsafe.Offsetof(t.n2_6), unsafe.Sizeof(t.n2_6), unsafe.Alignof(t.n2_6))
	fmt.Println("u16_7", unsafe.Offsetof(t.u16_7), unsafe.Sizeof(t.u16_7), unsafe.Alignof(t.u16_7))
	fmt.Println("i_8", unsafe.Offsetof(t.i_8), unsafe.Sizeof(t.i_8), unsafe.Alignof(t.i_8))
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
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_0))))
	fmt.Println(*(*uint64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u64_2))))
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_4))))
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_7))))
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_8))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.up_0, gT.c128_1, gT.u64_2, gT.n2_3, gT.b_4, gT.c64_5, gT.n2_6, gT.u16_7, gT.i_8, gT.z1_9)
	fmt.Println("directO", gO.pre, gO.up_0, gO.c128_1, gO.u64_2, gO.n2_3, gO.b_4, gO.c64_5, gO.n2_6, gO.u16_7, gO.i_8, gO.z1_9, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{up_0: 77, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, b_4: true, c64_5: 1 + 2i, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_7: 60000, i_8: -42, z1_9: struct{}{}}
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
	loc := T{up_0: 88, c128_1: 0.5 + 0.25i, u64_2: 77, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, b_4: false, c64_5: -3 - 4i, n2_6: N2{false, [3]byte{1, 1, 1}, -0.5}, u16_7: 513, i_8: 123456789, z1_9: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{up_0: 77, c128_1: 3.5 - 1i, u64_2: 18000000000000000000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, b_4: true, c64_5: 1 + 2i, n2_6: N2{true, [3]byte{9, 8, 7}, 2.5}, u16_7: 60000, i_8: -42, z1_9: struct{}{}}, dT == gT)
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
			res[i] = fmt.Sprint(v.up_0, v.c128_1, v.u64_2, v.n2_3, v.b_4, v.c64_5, v.n2_6, v.u16_7, v.i_8, v.z1_9, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
