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
	z0_0   [0]int
	r_1    rune
	sl_2   []int
	any_3  any
	any_4  any
	n1_5   N1
	c128_6 complex128
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.z0_0, x.r_1, x.sl_2, x.any_3, x.any_4, x.n1_5, x.c128_6)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.z0_0, x.r_1, x.sl_2, x.any_3, x.any_4, x.n1_5, x.c128_6)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.z0_0, x.in.r_1, x.in.sl_2, x.in.any_3, x.in.any_4, x.in.n1_5, x.in.c128_6, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.z0_0, x.in.r_1, x.in.sl_2, x.in.any_3, x.in.any_4, x.in.n1_5, x.in.c128_6, x.post)
}

var gT = T{z0_0: [0]int{}, r_1: 'x', sl_2: []int{1, 2, 3}, any_3: 42, any_4: 42, n1_5: N1{3, -7}, c128_6: 3.5 - 1i}
var gO = O{pre: 7, in: T{z0_0: [0]int{}, r_1: 'x', sl_2: []int{1, 2, 3}, any_3: 42, any_4: 42, n1_5: N1{3, -7}, c128_6: 3.5 - 1i}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{z0_0: [0]int{}, r_1: 'x', sl_2: []int{1, 2, 3}, any_3: 42, any_4: 42, n1_5: N1{3, -7}, c128_6: 3.5 - 1i}, {}, T{z0_0: [0]int{}, r_1: '世', sl_2: []int{9}, any_3: "dyn", any_4: "dyn", n1_5: N1{-3, 70000000000}, c128_6: 0.5 + 0.25i}}
var sT = []T{T{z0_0: [0]int{}, r_1: '世', sl_2: []int{9}, any_3: "dyn", any_4: "dyn", n1_5: N1{-3, 70000000000}, c128_6: 0.5 + 0.25i}, T{z0_0: [0]int{}, r_1: 'x', sl_2: []int{1, 2, 3}, any_3: 42, any_4: 42, n1_5: N1{3, -7}, c128_6: 3.5 - 1i}}
var aO = [2]O{O{pre: 1, in: T{z0_0: [0]int{}, r_1: 'x', sl_2: []int{1, 2, 3}, any_3: 42, any_4: 42, n1_5: N1{3, -7}, c128_6: 3.5 - 1i}, post: 2}, O{pre: 3, in: T{z0_0: [0]int{}, r_1: '世', sl_2: []int{9}, any_3: "dyn", any_4: "dyn", n1_5: N1{-3, 70000000000}, c128_6: 0.5 + 0.25i}, post: 4}}
var mT = map[int]T{}
var pField = &gT.z0_0
var pFieldO = &gO.in.z0_0
var pElem = &aT[2].z0_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.z0_0 = [0]int{}
		p.r_1 = '世'
		p.sl_2 = []int{9}
		p.any_3 = "dyn"
		p.any_4 = "dyn"
		p.n1_5 = N1{-3, 70000000000}
		p.c128_6 = 0.5 + 0.25i
	} else {
		p.z0_0 = [0]int{}
		p.r_1 = 'x'
		p.sl_2 = []int{1, 2, 3}
		p.any_3 = 42
		p.any_4 = 42
		p.n1_5 = N1{3, -7}
		p.c128_6 = 3.5 - 1i
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("z0_0", unsafe.Offsetof(t.z0_0), unsafe.Sizeof(t.z0_0), unsafe.Alignof(t.z0_0))
	fmt.Println("r_1", unsafe.Offsetof(t.r_1), unsafe.Sizeof(t.r_1), unsafe.Alignof(t.r_1))
	fmt.Println("sl_2", unsafe.Offsetof(t.sl_2), unsafe.Sizeof(t.sl_2), unsafe.Alignof(t.sl_2))
	fmt.Println("any_3", unsafe.Offsetof(t.any_3), unsafe.Sizeof(t.any_3), unsafe.Alignof(t.any_3))
	fmt.Println("any_4", unsafe.Offsetof(t.any_4), unsafe.Sizeof(t.any_4), unsafe.Alignof(t.any_4))
	fmt.Println("n1_5", unsafe.Offsetof(t.n1_5), unsafe.Sizeof(t.n1_5), unsafe.Alignof(t.n1_5))
	fmt.Println("c128_6", unsafe.Offsetof(t.c128_6), unsafe.Sizeof(t.c128_6), unsafe.Alignof(t.c128_6))
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
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_1))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.z0_0, gT.r_1, gT.sl_2, gT.any_3, gT.any_4, gT.n1_5, gT.c128_6)
	fmt.Println("directO", gO.pre, gO.in.z0_0, gO.in.r_1, gO.in.sl_2, gO.in.any_3, gO.in.any_4, gO.in.n1_5, gO.in.c128_6, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{z0_0: [0]int{}, r_1: 'x', sl_2: []int{1, 2, 3}, any_3: 42, any_4: 42, n1_5: N1{3, -7}, c128_6: 3.5 - 1i}
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
	loc := T{z0_0: [0]int{}, r_1: '世', sl_2: []int{9}, any_3: "dyn", any_4: "dyn", n1_5: N1{-3, 70000000000}, c128_6: 0.5 + 0.25i}
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
			res[i] = fmt.Sprint(v.z0_0, v.r_1, v.sl_2, v.any_3, v.any_4, v.n1_5, v.c128_6, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
