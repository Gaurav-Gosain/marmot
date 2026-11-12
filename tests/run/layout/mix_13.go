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
	u32_1 uint32
	p_2   *int
	s_3   string
	s_4   string
	f32_5 float32
	f32_6 float32
	i16_7 int16
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.z2_0, x.u32_1, *x.p_2, x.s_3, x.s_4, x.f32_5, x.f32_6, x.i16_7)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.z2_0, x.u32_1, *x.p_2, x.s_3, x.s_4, x.f32_5, x.f32_6, x.i16_7)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.z2_0, x.u32_1, *x.p_2, x.s_3, x.s_4, x.f32_5, x.f32_6, x.i16_7, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.z2_0, x.u32_1, *x.p_2, x.s_3, x.s_4, x.f32_5, x.f32_6, x.i16_7, x.post)
}

var gT = T{z2_0: [0]int64{}, u32_1: 4000000000, p_2: &ints[2], s_3: "héllo\t", s_4: "héllo\t", f32_5: 1.5, f32_6: 1.5, i16_7: -1234}
var gO = O{pre: 7, T: T{z2_0: [0]int64{}, u32_1: 4000000000, p_2: &ints[2], s_3: "héllo\t", s_4: "héllo\t", f32_5: 1.5, f32_6: 1.5, i16_7: -1234}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{z2_0: [0]int64{}, u32_1: 4000000000, p_2: &ints[2], s_3: "héllo\t", s_4: "héllo\t", f32_5: 1.5, f32_6: 1.5, i16_7: -1234}, {}, T{z2_0: [0]int64{}, u32_1: 99, p_2: &ints[5], s_3: "x", s_4: "x", f32_5: -2.75, f32_6: -2.75, i16_7: 4321}}
var sT = []T{T{z2_0: [0]int64{}, u32_1: 99, p_2: &ints[5], s_3: "x", s_4: "x", f32_5: -2.75, f32_6: -2.75, i16_7: 4321}, T{z2_0: [0]int64{}, u32_1: 4000000000, p_2: &ints[2], s_3: "héllo\t", s_4: "héllo\t", f32_5: 1.5, f32_6: 1.5, i16_7: -1234}}
var aO = [2]O{O{pre: 1, T: T{z2_0: [0]int64{}, u32_1: 4000000000, p_2: &ints[2], s_3: "héllo\t", s_4: "héllo\t", f32_5: 1.5, f32_6: 1.5, i16_7: -1234}, post: 2}, O{pre: 3, T: T{z2_0: [0]int64{}, u32_1: 99, p_2: &ints[5], s_3: "x", s_4: "x", f32_5: -2.75, f32_6: -2.75, i16_7: 4321}, post: 4}}
var mT = map[int]T{}
var pField = &gT.s_3
var pFieldO = &gO.s_3
var pElem = &aT[2].s_3
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.z2_0 = [0]int64{}
		p.u32_1 = 99
		p.p_2 = &ints[5]
		p.s_3 = "x"
		p.s_4 = "x"
		p.f32_5 = -2.75
		p.f32_6 = -2.75
		p.i16_7 = 4321
	} else {
		p.z2_0 = [0]int64{}
		p.u32_1 = 4000000000
		p.p_2 = &ints[2]
		p.s_3 = "héllo\t"
		p.s_4 = "héllo\t"
		p.f32_5 = 1.5
		p.f32_6 = 1.5
		p.i16_7 = -1234
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("z2_0", unsafe.Offsetof(t.z2_0), unsafe.Sizeof(t.z2_0), unsafe.Alignof(t.z2_0))
	fmt.Println("u32_1", unsafe.Offsetof(t.u32_1), unsafe.Sizeof(t.u32_1), unsafe.Alignof(t.u32_1))
	fmt.Println("p_2", unsafe.Offsetof(t.p_2), unsafe.Sizeof(t.p_2), unsafe.Alignof(t.p_2))
	fmt.Println("s_3", unsafe.Offsetof(t.s_3), unsafe.Sizeof(t.s_3), unsafe.Alignof(t.s_3))
	fmt.Println("s_4", unsafe.Offsetof(t.s_4), unsafe.Sizeof(t.s_4), unsafe.Alignof(t.s_4))
	fmt.Println("f32_5", unsafe.Offsetof(t.f32_5), unsafe.Sizeof(t.f32_5), unsafe.Alignof(t.f32_5))
	fmt.Println("f32_6", unsafe.Offsetof(t.f32_6), unsafe.Sizeof(t.f32_6), unsafe.Alignof(t.f32_6))
	fmt.Println("i16_7", unsafe.Offsetof(t.i16_7), unsafe.Sizeof(t.i16_7), unsafe.Alignof(t.i16_7))
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
	fmt.Println(*(*string)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.s_3))))
	fmt.Println(*(*string)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.s_4))))
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_5))))
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_6))))
	fmt.Println(*(*int16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i16_7))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.z2_0, gT.u32_1, *gT.p_2, gT.s_3, gT.s_4, gT.f32_5, gT.f32_6, gT.i16_7)
	fmt.Println("directO", gO.pre, gO.z2_0, gO.u32_1, *gO.p_2, gO.s_3, gO.s_4, gO.f32_5, gO.f32_6, gO.i16_7, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{z2_0: [0]int64{}, u32_1: 4000000000, p_2: &ints[2], s_3: "héllo\t", s_4: "héllo\t", f32_5: 1.5, f32_6: 1.5, i16_7: -1234}
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
	loc := T{z2_0: [0]int64{}, u32_1: 99, p_2: &ints[5], s_3: "x", s_4: "x", f32_5: -2.75, f32_6: -2.75, i16_7: 4321}
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
	fmt.Println(cp == gT, gT == T{z2_0: [0]int64{}, u32_1: 4000000000, p_2: &ints[2], s_3: "héllo\t", s_4: "héllo\t", f32_5: 1.5, f32_6: 1.5, i16_7: -1234}, dT == gT)
	cp.i16_7 = 4321
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
			res[i] = fmt.Sprint(v.z2_0, v.u32_1, *v.p_2, v.s_3, v.s_4, v.f32_5, v.f32_6, v.i16_7, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
