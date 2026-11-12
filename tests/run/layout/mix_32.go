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
	i_0   int
	u16_1 uint16
	p_2   *int
	a2_3  [2]int16
	any_4 any
	c64_5 complex64
	z1_6  struct{}
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.i_0, x.u16_1, *x.p_2, x.a2_3, x.any_4, x.c64_5, x.z1_6)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.i_0, x.u16_1, *x.p_2, x.a2_3, x.any_4, x.c64_5, x.z1_6)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.i_0, x.in.u16_1, *x.in.p_2, x.in.a2_3, x.in.any_4, x.in.c64_5, x.in.z1_6, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.i_0, x.in.u16_1, *x.in.p_2, x.in.a2_3, x.in.any_4, x.in.c64_5, x.in.z1_6, x.post)
}

var gT = T{i_0: -42, u16_1: 60000, p_2: &ints[2], a2_3: [2]int16{-1, 2}, any_4: 42, c64_5: 1 + 2i, z1_6: struct{}{}}
var gO = O{pre: 7, in: T{i_0: -42, u16_1: 60000, p_2: &ints[2], a2_3: [2]int16{-1, 2}, any_4: 42, c64_5: 1 + 2i, z1_6: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{i_0: -42, u16_1: 60000, p_2: &ints[2], a2_3: [2]int16{-1, 2}, any_4: 42, c64_5: 1 + 2i, z1_6: struct{}{}}, {}, T{i_0: 123456789, u16_1: 513, p_2: &ints[5], a2_3: [2]int16{300, -300}, any_4: "dyn", c64_5: -3 - 4i, z1_6: struct{}{}}}
var sT = []T{T{i_0: 123456789, u16_1: 513, p_2: &ints[5], a2_3: [2]int16{300, -300}, any_4: "dyn", c64_5: -3 - 4i, z1_6: struct{}{}}, T{i_0: -42, u16_1: 60000, p_2: &ints[2], a2_3: [2]int16{-1, 2}, any_4: 42, c64_5: 1 + 2i, z1_6: struct{}{}}}
var aO = [2]O{O{pre: 1, in: T{i_0: -42, u16_1: 60000, p_2: &ints[2], a2_3: [2]int16{-1, 2}, any_4: 42, c64_5: 1 + 2i, z1_6: struct{}{}}, post: 2}, O{pre: 3, in: T{i_0: 123456789, u16_1: 513, p_2: &ints[5], a2_3: [2]int16{300, -300}, any_4: "dyn", c64_5: -3 - 4i, z1_6: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i_0
var pFieldO = &gO.in.i_0
var pElem = &aT[2].i_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.i_0 = 123456789
		p.u16_1 = 513
		p.p_2 = &ints[5]
		p.a2_3 = [2]int16{300, -300}
		p.any_4 = "dyn"
		p.c64_5 = -3 - 4i
		p.z1_6 = struct{}{}
	} else {
		p.i_0 = -42
		p.u16_1 = 60000
		p.p_2 = &ints[2]
		p.a2_3 = [2]int16{-1, 2}
		p.any_4 = 42
		p.c64_5 = 1 + 2i
		p.z1_6 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("i_0", unsafe.Offsetof(t.i_0), unsafe.Sizeof(t.i_0), unsafe.Alignof(t.i_0))
	fmt.Println("u16_1", unsafe.Offsetof(t.u16_1), unsafe.Sizeof(t.u16_1), unsafe.Alignof(t.u16_1))
	fmt.Println("p_2", unsafe.Offsetof(t.p_2), unsafe.Sizeof(t.p_2), unsafe.Alignof(t.p_2))
	fmt.Println("a2_3", unsafe.Offsetof(t.a2_3), unsafe.Sizeof(t.a2_3), unsafe.Alignof(t.a2_3))
	fmt.Println("any_4", unsafe.Offsetof(t.any_4), unsafe.Sizeof(t.any_4), unsafe.Alignof(t.any_4))
	fmt.Println("c64_5", unsafe.Offsetof(t.c64_5), unsafe.Sizeof(t.c64_5), unsafe.Alignof(t.c64_5))
	fmt.Println("z1_6", unsafe.Offsetof(t.z1_6), unsafe.Sizeof(t.z1_6), unsafe.Alignof(t.z1_6))
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
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_0))))
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_1))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.i_0, gT.u16_1, *gT.p_2, gT.a2_3, gT.any_4, gT.c64_5, gT.z1_6)
	fmt.Println("directO", gO.pre, gO.in.i_0, gO.in.u16_1, *gO.in.p_2, gO.in.a2_3, gO.in.any_4, gO.in.c64_5, gO.in.z1_6, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{i_0: -42, u16_1: 60000, p_2: &ints[2], a2_3: [2]int16{-1, 2}, any_4: 42, c64_5: 1 + 2i, z1_6: struct{}{}}
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
	loc := T{i_0: 123456789, u16_1: 513, p_2: &ints[5], a2_3: [2]int16{300, -300}, any_4: "dyn", c64_5: -3 - 4i, z1_6: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{i_0: -42, u16_1: 60000, p_2: &ints[2], a2_3: [2]int16{-1, 2}, any_4: 42, c64_5: 1 + 2i, z1_6: struct{}{}}, dT == gT)
	cp.z1_6 = struct{}{}
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
			res[i] = fmt.Sprint(v.i_0, v.u16_1, *v.p_2, v.a2_3, v.any_4, v.c64_5, v.z1_6, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
