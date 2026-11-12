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
	i_1   int
	a3_2  [3]byte
	i16_3 int16
	u8_4  uint8
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.z2_0, x.i_1, x.a3_2, x.i16_3, x.u8_4)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.z2_0, x.i_1, x.a3_2, x.i16_3, x.u8_4)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.z2_0, x.in.i_1, x.in.a3_2, x.in.i16_3, x.in.u8_4, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.z2_0, x.in.i_1, x.in.a3_2, x.in.i16_3, x.in.u8_4, x.post)
}

var gT = T{z2_0: [0]int64{}, i_1: -42, a3_2: [3]byte{1, 2, 3}, i16_3: -1234, u8_4: 200}
var gO = O{pre: 7, in: T{z2_0: [0]int64{}, i_1: -42, a3_2: [3]byte{1, 2, 3}, i16_3: -1234, u8_4: 200}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{z2_0: [0]int64{}, i_1: -42, a3_2: [3]byte{1, 2, 3}, i16_3: -1234, u8_4: 200}, {}, T{z2_0: [0]int64{}, i_1: 123456789, a3_2: [3]byte{7, 8, 9}, i16_3: 4321, u8_4: 13}}
var sT = []T{T{z2_0: [0]int64{}, i_1: 123456789, a3_2: [3]byte{7, 8, 9}, i16_3: 4321, u8_4: 13}, T{z2_0: [0]int64{}, i_1: -42, a3_2: [3]byte{1, 2, 3}, i16_3: -1234, u8_4: 200}}
var aO = [2]O{O{pre: 1, in: T{z2_0: [0]int64{}, i_1: -42, a3_2: [3]byte{1, 2, 3}, i16_3: -1234, u8_4: 200}, post: 2}, O{pre: 3, in: T{z2_0: [0]int64{}, i_1: 123456789, a3_2: [3]byte{7, 8, 9}, i16_3: 4321, u8_4: 13}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i_1
var pFieldO = &gO.in.i_1
var pElem = &aT[2].i_1
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.z2_0 = [0]int64{}
		p.i_1 = 123456789
		p.a3_2 = [3]byte{7, 8, 9}
		p.i16_3 = 4321
		p.u8_4 = 13
	} else {
		p.z2_0 = [0]int64{}
		p.i_1 = -42
		p.a3_2 = [3]byte{1, 2, 3}
		p.i16_3 = -1234
		p.u8_4 = 200
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("z2_0", unsafe.Offsetof(t.z2_0), unsafe.Sizeof(t.z2_0), unsafe.Alignof(t.z2_0))
	fmt.Println("i_1", unsafe.Offsetof(t.i_1), unsafe.Sizeof(t.i_1), unsafe.Alignof(t.i_1))
	fmt.Println("a3_2", unsafe.Offsetof(t.a3_2), unsafe.Sizeof(t.a3_2), unsafe.Alignof(t.a3_2))
	fmt.Println("i16_3", unsafe.Offsetof(t.i16_3), unsafe.Sizeof(t.i16_3), unsafe.Alignof(t.i16_3))
	fmt.Println("u8_4", unsafe.Offsetof(t.u8_4), unsafe.Sizeof(t.u8_4), unsafe.Alignof(t.u8_4))
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
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_1))))
	fmt.Println(*(*int16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i16_3))))
	fmt.Println(*(*uint8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u8_4))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.z2_0, gT.i_1, gT.a3_2, gT.i16_3, gT.u8_4)
	fmt.Println("directO", gO.pre, gO.in.z2_0, gO.in.i_1, gO.in.a3_2, gO.in.i16_3, gO.in.u8_4, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{z2_0: [0]int64{}, i_1: -42, a3_2: [3]byte{1, 2, 3}, i16_3: -1234, u8_4: 200}
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
	loc := T{z2_0: [0]int64{}, i_1: 123456789, a3_2: [3]byte{7, 8, 9}, i16_3: 4321, u8_4: 13}
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
	fmt.Println(cp == gT, gT == T{z2_0: [0]int64{}, i_1: -42, a3_2: [3]byte{1, 2, 3}, i16_3: -1234, u8_4: 200}, dT == gT)
	cp.u8_4 = 13
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
			res[i] = fmt.Sprint(v.z2_0, v.i_1, v.a3_2, v.i16_3, v.u8_4, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
