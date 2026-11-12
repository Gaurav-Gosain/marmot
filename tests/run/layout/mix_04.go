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
	u32_1 uint32
	n1_2  N1
	u8_3  uint8
	p_4   *int
	z1_5  struct{}
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.n1_0, x.u32_1, x.n1_2, x.u8_3, *x.p_4, x.z1_5)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.n1_0, x.u32_1, x.n1_2, x.u8_3, *x.p_4, x.z1_5)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.n1_0, x.u32_1, x.n1_2, x.u8_3, *x.p_4, x.z1_5, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.n1_0, x.u32_1, x.n1_2, x.u8_3, *x.p_4, x.z1_5, x.post)
}

var gT = T{n1_0: N1{3, -7}, u32_1: 4000000000, n1_2: N1{3, -7}, u8_3: 200, p_4: &ints[2], z1_5: struct{}{}}
var gO = O{pre: 7, T: T{n1_0: N1{3, -7}, u32_1: 4000000000, n1_2: N1{3, -7}, u8_3: 200, p_4: &ints[2], z1_5: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{n1_0: N1{3, -7}, u32_1: 4000000000, n1_2: N1{3, -7}, u8_3: 200, p_4: &ints[2], z1_5: struct{}{}}, {}, T{n1_0: N1{-3, 70000000000}, u32_1: 99, n1_2: N1{-3, 70000000000}, u8_3: 13, p_4: &ints[5], z1_5: struct{}{}}}
var sT = []T{T{n1_0: N1{-3, 70000000000}, u32_1: 99, n1_2: N1{-3, 70000000000}, u8_3: 13, p_4: &ints[5], z1_5: struct{}{}}, T{n1_0: N1{3, -7}, u32_1: 4000000000, n1_2: N1{3, -7}, u8_3: 200, p_4: &ints[2], z1_5: struct{}{}}}
var aO = [2]O{O{pre: 1, T: T{n1_0: N1{3, -7}, u32_1: 4000000000, n1_2: N1{3, -7}, u8_3: 200, p_4: &ints[2], z1_5: struct{}{}}, post: 2}, O{pre: 3, T: T{n1_0: N1{-3, 70000000000}, u32_1: 99, n1_2: N1{-3, 70000000000}, u8_3: 13, p_4: &ints[5], z1_5: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.u8_3
var pFieldO = &gO.u8_3
var pElem = &aT[2].u8_3
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.n1_0 = N1{-3, 70000000000}
		p.u32_1 = 99
		p.n1_2 = N1{-3, 70000000000}
		p.u8_3 = 13
		p.p_4 = &ints[5]
		p.z1_5 = struct{}{}
	} else {
		p.n1_0 = N1{3, -7}
		p.u32_1 = 4000000000
		p.n1_2 = N1{3, -7}
		p.u8_3 = 200
		p.p_4 = &ints[2]
		p.z1_5 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("n1_0", unsafe.Offsetof(t.n1_0), unsafe.Sizeof(t.n1_0), unsafe.Alignof(t.n1_0))
	fmt.Println("u32_1", unsafe.Offsetof(t.u32_1), unsafe.Sizeof(t.u32_1), unsafe.Alignof(t.u32_1))
	fmt.Println("n1_2", unsafe.Offsetof(t.n1_2), unsafe.Sizeof(t.n1_2), unsafe.Alignof(t.n1_2))
	fmt.Println("u8_3", unsafe.Offsetof(t.u8_3), unsafe.Sizeof(t.u8_3), unsafe.Alignof(t.u8_3))
	fmt.Println("p_4", unsafe.Offsetof(t.p_4), unsafe.Sizeof(t.p_4), unsafe.Alignof(t.p_4))
	fmt.Println("z1_5", unsafe.Offsetof(t.z1_5), unsafe.Sizeof(t.z1_5), unsafe.Alignof(t.z1_5))
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
	fmt.Println(*(*uint32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u32_1))))
	fmt.Println(*(*uint8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u8_3))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.n1_0, gT.u32_1, gT.n1_2, gT.u8_3, *gT.p_4, gT.z1_5)
	fmt.Println("directO", gO.pre, gO.n1_0, gO.u32_1, gO.n1_2, gO.u8_3, *gO.p_4, gO.z1_5, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{n1_0: N1{3, -7}, u32_1: 4000000000, n1_2: N1{3, -7}, u8_3: 200, p_4: &ints[2], z1_5: struct{}{}}
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
	loc := T{n1_0: N1{-3, 70000000000}, u32_1: 99, n1_2: N1{-3, 70000000000}, u8_3: 13, p_4: &ints[5], z1_5: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{n1_0: N1{3, -7}, u32_1: 4000000000, n1_2: N1{3, -7}, u8_3: 200, p_4: &ints[2], z1_5: struct{}{}}, dT == gT)
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
			res[i] = fmt.Sprint(v.n1_0, v.u32_1, v.n1_2, v.u8_3, *v.p_4, v.z1_5, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
