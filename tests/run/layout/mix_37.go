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
	z2_0  [0]int64
	i16_1 int16
	i32_2 int32
	n2_3  N2
	p_4   *int
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.z2_0, x.i16_1, x.i32_2, x.n2_3, *x.p_4)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.z2_0, x.i16_1, x.i32_2, x.n2_3, *x.p_4)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.z2_0, x.i16_1, x.i32_2, x.n2_3, *x.p_4, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.z2_0, x.i16_1, x.i32_2, x.n2_3, *x.p_4, x.post)
}

var gT = T{z2_0: [0]int64{}, i16_1: -1234, i32_2: -100000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, p_4: &ints[2]}
var gO = O{pre: 7, T: T{z2_0: [0]int64{}, i16_1: -1234, i32_2: -100000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, p_4: &ints[2]}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{z2_0: [0]int64{}, i16_1: -1234, i32_2: -100000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, p_4: &ints[2]}, {}, T{z2_0: [0]int64{}, i16_1: 4321, i32_2: 70000, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, p_4: &ints[5]}}
var sT = []T{T{z2_0: [0]int64{}, i16_1: 4321, i32_2: 70000, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, p_4: &ints[5]}, T{z2_0: [0]int64{}, i16_1: -1234, i32_2: -100000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, p_4: &ints[2]}}
var aO = [2]O{O{pre: 1, T: T{z2_0: [0]int64{}, i16_1: -1234, i32_2: -100000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, p_4: &ints[2]}, post: 2}, O{pre: 3, T: T{z2_0: [0]int64{}, i16_1: 4321, i32_2: 70000, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, p_4: &ints[5]}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i32_2
var pFieldO = &gO.i32_2
var pElem = &aT[2].i32_2
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.z2_0 = [0]int64{}
		p.i16_1 = 4321
		p.i32_2 = 70000
		p.n2_3 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.p_4 = &ints[5]
	} else {
		p.z2_0 = [0]int64{}
		p.i16_1 = -1234
		p.i32_2 = -100000
		p.n2_3 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.p_4 = &ints[2]
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("z2_0", unsafe.Offsetof(t.z2_0), unsafe.Sizeof(t.z2_0), unsafe.Alignof(t.z2_0))
	fmt.Println("i16_1", unsafe.Offsetof(t.i16_1), unsafe.Sizeof(t.i16_1), unsafe.Alignof(t.i16_1))
	fmt.Println("i32_2", unsafe.Offsetof(t.i32_2), unsafe.Sizeof(t.i32_2), unsafe.Alignof(t.i32_2))
	fmt.Println("n2_3", unsafe.Offsetof(t.n2_3), unsafe.Sizeof(t.n2_3), unsafe.Alignof(t.n2_3))
	fmt.Println("p_4", unsafe.Offsetof(t.p_4), unsafe.Sizeof(t.p_4), unsafe.Alignof(t.p_4))
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
	fmt.Println(*(*int32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i32_2))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.z2_0, gT.i16_1, gT.i32_2, gT.n2_3, *gT.p_4)
	fmt.Println("directO", gO.pre, gO.z2_0, gO.i16_1, gO.i32_2, gO.n2_3, *gO.p_4, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{z2_0: [0]int64{}, i16_1: -1234, i32_2: -100000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, p_4: &ints[2]}
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
	loc := T{z2_0: [0]int64{}, i16_1: 4321, i32_2: 70000, n2_3: N2{false, [3]byte{1, 1, 1}, -0.5}, p_4: &ints[5]}
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
	fmt.Println(cp == gT, gT == T{z2_0: [0]int64{}, i16_1: -1234, i32_2: -100000, n2_3: N2{true, [3]byte{9, 8, 7}, 2.5}, p_4: &ints[2]}, dT == gT)
	cp.p_4 = &ints[5]
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
			res[i] = fmt.Sprint(v.z2_0, v.i16_1, v.i32_2, v.n2_3, *v.p_4, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
