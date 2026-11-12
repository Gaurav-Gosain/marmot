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
	any_1 any
	z2_2  [0]int64
	a2_3  [2]int16
	c64_4 complex64
	i8_5  int8
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.n1_0, x.any_1, x.z2_2, x.a2_3, x.c64_4, x.i8_5)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.n1_0, x.any_1, x.z2_2, x.a2_3, x.c64_4, x.i8_5)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.n1_0, x.any_1, x.z2_2, x.a2_3, x.c64_4, x.i8_5, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.n1_0, x.any_1, x.z2_2, x.a2_3, x.c64_4, x.i8_5, x.post)
}

var gT = T{n1_0: N1{3, -7}, any_1: 42, z2_2: [0]int64{}, a2_3: [2]int16{-1, 2}, c64_4: 1 + 2i, i8_5: -5}
var gO = O{pre: 7, T: T{n1_0: N1{3, -7}, any_1: 42, z2_2: [0]int64{}, a2_3: [2]int16{-1, 2}, c64_4: 1 + 2i, i8_5: -5}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{n1_0: N1{3, -7}, any_1: 42, z2_2: [0]int64{}, a2_3: [2]int16{-1, 2}, c64_4: 1 + 2i, i8_5: -5}, {}, T{n1_0: N1{-3, 70000000000}, any_1: "dyn", z2_2: [0]int64{}, a2_3: [2]int16{300, -300}, c64_4: -3 - 4i, i8_5: 7}}
var sT = []T{T{n1_0: N1{-3, 70000000000}, any_1: "dyn", z2_2: [0]int64{}, a2_3: [2]int16{300, -300}, c64_4: -3 - 4i, i8_5: 7}, T{n1_0: N1{3, -7}, any_1: 42, z2_2: [0]int64{}, a2_3: [2]int16{-1, 2}, c64_4: 1 + 2i, i8_5: -5}}
var aO = [2]O{O{pre: 1, T: T{n1_0: N1{3, -7}, any_1: 42, z2_2: [0]int64{}, a2_3: [2]int16{-1, 2}, c64_4: 1 + 2i, i8_5: -5}, post: 2}, O{pre: 3, T: T{n1_0: N1{-3, 70000000000}, any_1: "dyn", z2_2: [0]int64{}, a2_3: [2]int16{300, -300}, c64_4: -3 - 4i, i8_5: 7}, post: 4}}
var mT = map[int]T{}
var pField = &gT.n1_0
var pFieldO = &gO.n1_0
var pElem = &aT[2].n1_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.n1_0 = N1{-3, 70000000000}
		p.any_1 = "dyn"
		p.z2_2 = [0]int64{}
		p.a2_3 = [2]int16{300, -300}
		p.c64_4 = -3 - 4i
		p.i8_5 = 7
	} else {
		p.n1_0 = N1{3, -7}
		p.any_1 = 42
		p.z2_2 = [0]int64{}
		p.a2_3 = [2]int16{-1, 2}
		p.c64_4 = 1 + 2i
		p.i8_5 = -5
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("n1_0", unsafe.Offsetof(t.n1_0), unsafe.Sizeof(t.n1_0), unsafe.Alignof(t.n1_0))
	fmt.Println("any_1", unsafe.Offsetof(t.any_1), unsafe.Sizeof(t.any_1), unsafe.Alignof(t.any_1))
	fmt.Println("z2_2", unsafe.Offsetof(t.z2_2), unsafe.Sizeof(t.z2_2), unsafe.Alignof(t.z2_2))
	fmt.Println("a2_3", unsafe.Offsetof(t.a2_3), unsafe.Sizeof(t.a2_3), unsafe.Alignof(t.a2_3))
	fmt.Println("c64_4", unsafe.Offsetof(t.c64_4), unsafe.Sizeof(t.c64_4), unsafe.Alignof(t.c64_4))
	fmt.Println("i8_5", unsafe.Offsetof(t.i8_5), unsafe.Sizeof(t.i8_5), unsafe.Alignof(t.i8_5))
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
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_5))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.n1_0, gT.any_1, gT.z2_2, gT.a2_3, gT.c64_4, gT.i8_5)
	fmt.Println("directO", gO.pre, gO.n1_0, gO.any_1, gO.z2_2, gO.a2_3, gO.c64_4, gO.i8_5, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{n1_0: N1{3, -7}, any_1: 42, z2_2: [0]int64{}, a2_3: [2]int16{-1, 2}, c64_4: 1 + 2i, i8_5: -5}
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
	loc := T{n1_0: N1{-3, 70000000000}, any_1: "dyn", z2_2: [0]int64{}, a2_3: [2]int16{300, -300}, c64_4: -3 - 4i, i8_5: 7}
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
	fmt.Println(cp == gT, gT == T{n1_0: N1{3, -7}, any_1: 42, z2_2: [0]int64{}, a2_3: [2]int16{-1, 2}, c64_4: 1 + 2i, i8_5: -5}, dT == gT)
	cp.i8_5 = 7
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
			res[i] = fmt.Sprint(v.n1_0, v.any_1, v.z2_2, v.a2_3, v.c64_4, v.i8_5, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
