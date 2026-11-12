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
	z0_0  [0]int
	p_1   *int
	b_2   bool
	a3_3  [3]byte
	p_4   *int
	i16_5 int16
	c64_6 complex64
	n2_7  N2
	b_8   bool
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.z0_0, *x.p_1, x.b_2, x.a3_3, *x.p_4, x.i16_5, x.c64_6, x.n2_7, x.b_8)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.z0_0, *x.p_1, x.b_2, x.a3_3, *x.p_4, x.i16_5, x.c64_6, x.n2_7, x.b_8)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.z0_0, *x.p_1, x.b_2, x.a3_3, *x.p_4, x.i16_5, x.c64_6, x.n2_7, x.b_8, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.z0_0, *x.p_1, x.b_2, x.a3_3, *x.p_4, x.i16_5, x.c64_6, x.n2_7, x.b_8, x.post)
}

var gT = T{z0_0: [0]int{}, p_1: &ints[2], b_2: true, a3_3: [3]byte{1, 2, 3}, p_4: &ints[2], i16_5: -1234, c64_6: 1 + 2i, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, b_8: true}
var gO = O{pre: 7, T: T{z0_0: [0]int{}, p_1: &ints[2], b_2: true, a3_3: [3]byte{1, 2, 3}, p_4: &ints[2], i16_5: -1234, c64_6: 1 + 2i, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, b_8: true}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{z0_0: [0]int{}, p_1: &ints[2], b_2: true, a3_3: [3]byte{1, 2, 3}, p_4: &ints[2], i16_5: -1234, c64_6: 1 + 2i, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, b_8: true}, {}, T{z0_0: [0]int{}, p_1: &ints[5], b_2: false, a3_3: [3]byte{7, 8, 9}, p_4: &ints[5], i16_5: 4321, c64_6: -3 - 4i, n2_7: N2{false, [3]byte{1, 1, 1}, -0.5}, b_8: false}}
var sT = []T{T{z0_0: [0]int{}, p_1: &ints[5], b_2: false, a3_3: [3]byte{7, 8, 9}, p_4: &ints[5], i16_5: 4321, c64_6: -3 - 4i, n2_7: N2{false, [3]byte{1, 1, 1}, -0.5}, b_8: false}, T{z0_0: [0]int{}, p_1: &ints[2], b_2: true, a3_3: [3]byte{1, 2, 3}, p_4: &ints[2], i16_5: -1234, c64_6: 1 + 2i, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, b_8: true}}
var aO = [2]O{O{pre: 1, T: T{z0_0: [0]int{}, p_1: &ints[2], b_2: true, a3_3: [3]byte{1, 2, 3}, p_4: &ints[2], i16_5: -1234, c64_6: 1 + 2i, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, b_8: true}, post: 2}, O{pre: 3, T: T{z0_0: [0]int{}, p_1: &ints[5], b_2: false, a3_3: [3]byte{7, 8, 9}, p_4: &ints[5], i16_5: 4321, c64_6: -3 - 4i, n2_7: N2{false, [3]byte{1, 1, 1}, -0.5}, b_8: false}, post: 4}}
var mT = map[int]T{}
var pField = &gT.z0_0
var pFieldO = &gO.z0_0
var pElem = &aT[2].z0_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.z0_0 = [0]int{}
		p.p_1 = &ints[5]
		p.b_2 = false
		p.a3_3 = [3]byte{7, 8, 9}
		p.p_4 = &ints[5]
		p.i16_5 = 4321
		p.c64_6 = -3 - 4i
		p.n2_7 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.b_8 = false
	} else {
		p.z0_0 = [0]int{}
		p.p_1 = &ints[2]
		p.b_2 = true
		p.a3_3 = [3]byte{1, 2, 3}
		p.p_4 = &ints[2]
		p.i16_5 = -1234
		p.c64_6 = 1 + 2i
		p.n2_7 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.b_8 = true
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("z0_0", unsafe.Offsetof(t.z0_0), unsafe.Sizeof(t.z0_0), unsafe.Alignof(t.z0_0))
	fmt.Println("p_1", unsafe.Offsetof(t.p_1), unsafe.Sizeof(t.p_1), unsafe.Alignof(t.p_1))
	fmt.Println("b_2", unsafe.Offsetof(t.b_2), unsafe.Sizeof(t.b_2), unsafe.Alignof(t.b_2))
	fmt.Println("a3_3", unsafe.Offsetof(t.a3_3), unsafe.Sizeof(t.a3_3), unsafe.Alignof(t.a3_3))
	fmt.Println("p_4", unsafe.Offsetof(t.p_4), unsafe.Sizeof(t.p_4), unsafe.Alignof(t.p_4))
	fmt.Println("i16_5", unsafe.Offsetof(t.i16_5), unsafe.Sizeof(t.i16_5), unsafe.Alignof(t.i16_5))
	fmt.Println("c64_6", unsafe.Offsetof(t.c64_6), unsafe.Sizeof(t.c64_6), unsafe.Alignof(t.c64_6))
	fmt.Println("n2_7", unsafe.Offsetof(t.n2_7), unsafe.Sizeof(t.n2_7), unsafe.Alignof(t.n2_7))
	fmt.Println("b_8", unsafe.Offsetof(t.b_8), unsafe.Sizeof(t.b_8), unsafe.Alignof(t.b_8))
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
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_2))))
	fmt.Println(*(*int16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i16_5))))
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_8))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.z0_0, *gT.p_1, gT.b_2, gT.a3_3, *gT.p_4, gT.i16_5, gT.c64_6, gT.n2_7, gT.b_8)
	fmt.Println("directO", gO.pre, gO.z0_0, *gO.p_1, gO.b_2, gO.a3_3, *gO.p_4, gO.i16_5, gO.c64_6, gO.n2_7, gO.b_8, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{z0_0: [0]int{}, p_1: &ints[2], b_2: true, a3_3: [3]byte{1, 2, 3}, p_4: &ints[2], i16_5: -1234, c64_6: 1 + 2i, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, b_8: true}
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
	loc := T{z0_0: [0]int{}, p_1: &ints[5], b_2: false, a3_3: [3]byte{7, 8, 9}, p_4: &ints[5], i16_5: 4321, c64_6: -3 - 4i, n2_7: N2{false, [3]byte{1, 1, 1}, -0.5}, b_8: false}
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
	fmt.Println(cp == gT, gT == T{z0_0: [0]int{}, p_1: &ints[2], b_2: true, a3_3: [3]byte{1, 2, 3}, p_4: &ints[2], i16_5: -1234, c64_6: 1 + 2i, n2_7: N2{true, [3]byte{9, 8, 7}, 2.5}, b_8: true}, dT == gT)
	cp.b_8 = false
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
			res[i] = fmt.Sprint(v.z0_0, *v.p_1, v.b_2, v.a3_3, *v.p_4, v.i16_5, v.c64_6, v.n2_7, v.b_8, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
