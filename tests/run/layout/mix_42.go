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
	n2_0  N2
	r_1   rune
	up_2  uintptr
	r_3   rune
	z2_4  [0]int64
	f64_5 float64
	i16_6 int16
	u16_7 uint16
	s_8   string
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.n2_0, x.r_1, x.up_2, x.r_3, x.z2_4, x.f64_5, x.i16_6, x.u16_7, x.s_8)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.n2_0, x.r_1, x.up_2, x.r_3, x.z2_4, x.f64_5, x.i16_6, x.u16_7, x.s_8)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.n2_0, x.in.r_1, x.in.up_2, x.in.r_3, x.in.z2_4, x.in.f64_5, x.in.i16_6, x.in.u16_7, x.in.s_8, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.n2_0, x.in.r_1, x.in.up_2, x.in.r_3, x.in.z2_4, x.in.f64_5, x.in.i16_6, x.in.u16_7, x.in.s_8, x.post)
}

var gT = T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, r_1: 'x', up_2: 77, r_3: 'x', z2_4: [0]int64{}, f64_5: -2.25, i16_6: -1234, u16_7: 60000, s_8: "héllo\t"}
var gO = O{pre: 7, in: T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, r_1: 'x', up_2: 77, r_3: 'x', z2_4: [0]int64{}, f64_5: -2.25, i16_6: -1234, u16_7: 60000, s_8: "héllo\t"}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, r_1: 'x', up_2: 77, r_3: 'x', z2_4: [0]int64{}, f64_5: -2.25, i16_6: -1234, u16_7: 60000, s_8: "héllo\t"}, {}, T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, r_1: '世', up_2: 88, r_3: '世', z2_4: [0]int64{}, f64_5: 1e100, i16_6: 4321, u16_7: 513, s_8: "x"}}
var sT = []T{T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, r_1: '世', up_2: 88, r_3: '世', z2_4: [0]int64{}, f64_5: 1e100, i16_6: 4321, u16_7: 513, s_8: "x"}, T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, r_1: 'x', up_2: 77, r_3: 'x', z2_4: [0]int64{}, f64_5: -2.25, i16_6: -1234, u16_7: 60000, s_8: "héllo\t"}}
var aO = [2]O{O{pre: 1, in: T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, r_1: 'x', up_2: 77, r_3: 'x', z2_4: [0]int64{}, f64_5: -2.25, i16_6: -1234, u16_7: 60000, s_8: "héllo\t"}, post: 2}, O{pre: 3, in: T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, r_1: '世', up_2: 88, r_3: '世', z2_4: [0]int64{}, f64_5: 1e100, i16_6: 4321, u16_7: 513, s_8: "x"}, post: 4}}
var mT = map[int]T{}
var pField = &gT.f64_5
var pFieldO = &gO.in.f64_5
var pElem = &aT[2].f64_5
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.n2_0 = N2{false, [3]byte{1, 1, 1}, -0.5}
		p.r_1 = '世'
		p.up_2 = 88
		p.r_3 = '世'
		p.z2_4 = [0]int64{}
		p.f64_5 = 1e100
		p.i16_6 = 4321
		p.u16_7 = 513
		p.s_8 = "x"
	} else {
		p.n2_0 = N2{true, [3]byte{9, 8, 7}, 2.5}
		p.r_1 = 'x'
		p.up_2 = 77
		p.r_3 = 'x'
		p.z2_4 = [0]int64{}
		p.f64_5 = -2.25
		p.i16_6 = -1234
		p.u16_7 = 60000
		p.s_8 = "héllo\t"
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("n2_0", unsafe.Offsetof(t.n2_0), unsafe.Sizeof(t.n2_0), unsafe.Alignof(t.n2_0))
	fmt.Println("r_1", unsafe.Offsetof(t.r_1), unsafe.Sizeof(t.r_1), unsafe.Alignof(t.r_1))
	fmt.Println("up_2", unsafe.Offsetof(t.up_2), unsafe.Sizeof(t.up_2), unsafe.Alignof(t.up_2))
	fmt.Println("r_3", unsafe.Offsetof(t.r_3), unsafe.Sizeof(t.r_3), unsafe.Alignof(t.r_3))
	fmt.Println("z2_4", unsafe.Offsetof(t.z2_4), unsafe.Sizeof(t.z2_4), unsafe.Alignof(t.z2_4))
	fmt.Println("f64_5", unsafe.Offsetof(t.f64_5), unsafe.Sizeof(t.f64_5), unsafe.Alignof(t.f64_5))
	fmt.Println("i16_6", unsafe.Offsetof(t.i16_6), unsafe.Sizeof(t.i16_6), unsafe.Alignof(t.i16_6))
	fmt.Println("u16_7", unsafe.Offsetof(t.u16_7), unsafe.Sizeof(t.u16_7), unsafe.Alignof(t.u16_7))
	fmt.Println("s_8", unsafe.Offsetof(t.s_8), unsafe.Sizeof(t.s_8), unsafe.Alignof(t.s_8))
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
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_2))))
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_3))))
	fmt.Println(*(*float64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f64_5))))
	fmt.Println(*(*int16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i16_6))))
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_7))))
	fmt.Println(*(*string)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.s_8))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.n2_0, gT.r_1, gT.up_2, gT.r_3, gT.z2_4, gT.f64_5, gT.i16_6, gT.u16_7, gT.s_8)
	fmt.Println("directO", gO.pre, gO.in.n2_0, gO.in.r_1, gO.in.up_2, gO.in.r_3, gO.in.z2_4, gO.in.f64_5, gO.in.i16_6, gO.in.u16_7, gO.in.s_8, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, r_1: 'x', up_2: 77, r_3: 'x', z2_4: [0]int64{}, f64_5: -2.25, i16_6: -1234, u16_7: 60000, s_8: "héllo\t"}
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
	loc := T{n2_0: N2{false, [3]byte{1, 1, 1}, -0.5}, r_1: '世', up_2: 88, r_3: '世', z2_4: [0]int64{}, f64_5: 1e100, i16_6: 4321, u16_7: 513, s_8: "x"}
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
	fmt.Println(cp == gT, gT == T{n2_0: N2{true, [3]byte{9, 8, 7}, 2.5}, r_1: 'x', up_2: 77, r_3: 'x', z2_4: [0]int64{}, f64_5: -2.25, i16_6: -1234, u16_7: 60000, s_8: "héllo\t"}, dT == gT)
	cp.s_8 = "x"
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
			res[i] = fmt.Sprint(v.n2_0, v.r_1, v.up_2, v.r_3, v.z2_4, v.f64_5, v.i16_6, v.u16_7, v.s_8, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
