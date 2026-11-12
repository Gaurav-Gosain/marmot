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
	n1_0 N1
	sl_1 []int
	z1_2 struct{}
	b_3  bool
	a2_4 [2]int16
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.n1_0, x.sl_1, x.z1_2, x.b_3, x.a2_4)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.n1_0, x.sl_1, x.z1_2, x.b_3, x.a2_4)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.n1_0, x.in.sl_1, x.in.z1_2, x.in.b_3, x.in.a2_4, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.n1_0, x.in.sl_1, x.in.z1_2, x.in.b_3, x.in.a2_4, x.post)
}

var gT = T{n1_0: N1{3, -7}, sl_1: []int{1, 2, 3}, z1_2: struct{}{}, b_3: true, a2_4: [2]int16{-1, 2}}
var gO = O{pre: 7, in: T{n1_0: N1{3, -7}, sl_1: []int{1, 2, 3}, z1_2: struct{}{}, b_3: true, a2_4: [2]int16{-1, 2}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{n1_0: N1{3, -7}, sl_1: []int{1, 2, 3}, z1_2: struct{}{}, b_3: true, a2_4: [2]int16{-1, 2}}, {}, T{n1_0: N1{-3, 70000000000}, sl_1: []int{9}, z1_2: struct{}{}, b_3: false, a2_4: [2]int16{300, -300}}}
var sT = []T{T{n1_0: N1{-3, 70000000000}, sl_1: []int{9}, z1_2: struct{}{}, b_3: false, a2_4: [2]int16{300, -300}}, T{n1_0: N1{3, -7}, sl_1: []int{1, 2, 3}, z1_2: struct{}{}, b_3: true, a2_4: [2]int16{-1, 2}}}
var aO = [2]O{O{pre: 1, in: T{n1_0: N1{3, -7}, sl_1: []int{1, 2, 3}, z1_2: struct{}{}, b_3: true, a2_4: [2]int16{-1, 2}}, post: 2}, O{pre: 3, in: T{n1_0: N1{-3, 70000000000}, sl_1: []int{9}, z1_2: struct{}{}, b_3: false, a2_4: [2]int16{300, -300}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.n1_0
var pFieldO = &gO.in.n1_0
var pElem = &aT[2].n1_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.n1_0 = N1{-3, 70000000000}
		p.sl_1 = []int{9}
		p.z1_2 = struct{}{}
		p.b_3 = false
		p.a2_4 = [2]int16{300, -300}
	} else {
		p.n1_0 = N1{3, -7}
		p.sl_1 = []int{1, 2, 3}
		p.z1_2 = struct{}{}
		p.b_3 = true
		p.a2_4 = [2]int16{-1, 2}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("n1_0", unsafe.Offsetof(t.n1_0), unsafe.Sizeof(t.n1_0), unsafe.Alignof(t.n1_0))
	fmt.Println("sl_1", unsafe.Offsetof(t.sl_1), unsafe.Sizeof(t.sl_1), unsafe.Alignof(t.sl_1))
	fmt.Println("z1_2", unsafe.Offsetof(t.z1_2), unsafe.Sizeof(t.z1_2), unsafe.Alignof(t.z1_2))
	fmt.Println("b_3", unsafe.Offsetof(t.b_3), unsafe.Sizeof(t.b_3), unsafe.Alignof(t.b_3))
	fmt.Println("a2_4", unsafe.Offsetof(t.a2_4), unsafe.Sizeof(t.a2_4), unsafe.Alignof(t.a2_4))
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
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_3))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.n1_0, gT.sl_1, gT.z1_2, gT.b_3, gT.a2_4)
	fmt.Println("directO", gO.pre, gO.in.n1_0, gO.in.sl_1, gO.in.z1_2, gO.in.b_3, gO.in.a2_4, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{n1_0: N1{3, -7}, sl_1: []int{1, 2, 3}, z1_2: struct{}{}, b_3: true, a2_4: [2]int16{-1, 2}}
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
	loc := T{n1_0: N1{-3, 70000000000}, sl_1: []int{9}, z1_2: struct{}{}, b_3: false, a2_4: [2]int16{300, -300}}
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
			res[i] = fmt.Sprint(v.n1_0, v.sl_1, v.z1_2, v.b_3, v.a2_4, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
