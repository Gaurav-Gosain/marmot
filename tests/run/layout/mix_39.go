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
	u16_1 uint16
	i64_2 int64
	c64_3 complex64
	b_4   bool
	z1_5  struct{}
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.u32_0, x.u16_1, x.i64_2, x.c64_3, x.b_4, x.z1_5)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.u32_0, x.u16_1, x.i64_2, x.c64_3, x.b_4, x.z1_5)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.u32_0, x.in.u16_1, x.in.i64_2, x.in.c64_3, x.in.b_4, x.in.z1_5, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.u32_0, x.in.u16_1, x.in.i64_2, x.in.c64_3, x.in.b_4, x.in.z1_5, x.post)
}

var gT = T{u32_0: 4000000000, u16_1: 60000, i64_2: -9000000000, c64_3: 1 + 2i, b_4: true, z1_5: struct{}{}}
var gO = O{pre: 7, in: T{u32_0: 4000000000, u16_1: 60000, i64_2: -9000000000, c64_3: 1 + 2i, b_4: true, z1_5: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{u32_0: 4000000000, u16_1: 60000, i64_2: -9000000000, c64_3: 1 + 2i, b_4: true, z1_5: struct{}{}}, {}, T{u32_0: 99, u16_1: 513, i64_2: 8000000000, c64_3: -3 - 4i, b_4: false, z1_5: struct{}{}}}
var sT = []T{T{u32_0: 99, u16_1: 513, i64_2: 8000000000, c64_3: -3 - 4i, b_4: false, z1_5: struct{}{}}, T{u32_0: 4000000000, u16_1: 60000, i64_2: -9000000000, c64_3: 1 + 2i, b_4: true, z1_5: struct{}{}}}
var aO = [2]O{O{pre: 1, in: T{u32_0: 4000000000, u16_1: 60000, i64_2: -9000000000, c64_3: 1 + 2i, b_4: true, z1_5: struct{}{}}, post: 2}, O{pre: 3, in: T{u32_0: 99, u16_1: 513, i64_2: 8000000000, c64_3: -3 - 4i, b_4: false, z1_5: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i64_2
var pFieldO = &gO.in.i64_2
var pElem = &aT[2].i64_2
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.u32_0 = 99
		p.u16_1 = 513
		p.i64_2 = 8000000000
		p.c64_3 = -3 - 4i
		p.b_4 = false
		p.z1_5 = struct{}{}
	} else {
		p.u32_0 = 4000000000
		p.u16_1 = 60000
		p.i64_2 = -9000000000
		p.c64_3 = 1 + 2i
		p.b_4 = true
		p.z1_5 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("u32_0", unsafe.Offsetof(t.u32_0), unsafe.Sizeof(t.u32_0), unsafe.Alignof(t.u32_0))
	fmt.Println("u16_1", unsafe.Offsetof(t.u16_1), unsafe.Sizeof(t.u16_1), unsafe.Alignof(t.u16_1))
	fmt.Println("i64_2", unsafe.Offsetof(t.i64_2), unsafe.Sizeof(t.i64_2), unsafe.Alignof(t.i64_2))
	fmt.Println("c64_3", unsafe.Offsetof(t.c64_3), unsafe.Sizeof(t.c64_3), unsafe.Alignof(t.c64_3))
	fmt.Println("b_4", unsafe.Offsetof(t.b_4), unsafe.Sizeof(t.b_4), unsafe.Alignof(t.b_4))
	fmt.Println("z1_5", unsafe.Offsetof(t.z1_5), unsafe.Sizeof(t.z1_5), unsafe.Alignof(t.z1_5))
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
	fmt.Println(*(*uint32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u32_0))))
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_1))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_2))))
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_4))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.u32_0, gT.u16_1, gT.i64_2, gT.c64_3, gT.b_4, gT.z1_5)
	fmt.Println("directO", gO.pre, gO.in.u32_0, gO.in.u16_1, gO.in.i64_2, gO.in.c64_3, gO.in.b_4, gO.in.z1_5, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{u32_0: 4000000000, u16_1: 60000, i64_2: -9000000000, c64_3: 1 + 2i, b_4: true, z1_5: struct{}{}}
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
	loc := T{u32_0: 99, u16_1: 513, i64_2: 8000000000, c64_3: -3 - 4i, b_4: false, z1_5: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{u32_0: 4000000000, u16_1: 60000, i64_2: -9000000000, c64_3: 1 + 2i, b_4: true, z1_5: struct{}{}}, dT == gT)
	cp.z1_5 = struct{}{}
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
			res[i] = fmt.Sprint(v.u32_0, v.u16_1, v.i64_2, v.c64_3, v.b_4, v.z1_5, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
