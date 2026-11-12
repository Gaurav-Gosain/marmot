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
	b_0   bool
	p_1   *int
	i64_2 int64
	f32_3 float32
	u32_4 uint32
	r_5   rune
	i_6   int
	z1_7  struct{}
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.b_0, *x.p_1, x.i64_2, x.f32_3, x.u32_4, x.r_5, x.i_6, x.z1_7)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.b_0, *x.p_1, x.i64_2, x.f32_3, x.u32_4, x.r_5, x.i_6, x.z1_7)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.b_0, *x.in.p_1, x.in.i64_2, x.in.f32_3, x.in.u32_4, x.in.r_5, x.in.i_6, x.in.z1_7, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.b_0, *x.in.p_1, x.in.i64_2, x.in.f32_3, x.in.u32_4, x.in.r_5, x.in.i_6, x.in.z1_7, x.post)
}

var gT = T{b_0: true, p_1: &ints[2], i64_2: -9000000000, f32_3: 1.5, u32_4: 4000000000, r_5: 'x', i_6: -42, z1_7: struct{}{}}
var gO = O{pre: 7, in: T{b_0: true, p_1: &ints[2], i64_2: -9000000000, f32_3: 1.5, u32_4: 4000000000, r_5: 'x', i_6: -42, z1_7: struct{}{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{b_0: true, p_1: &ints[2], i64_2: -9000000000, f32_3: 1.5, u32_4: 4000000000, r_5: 'x', i_6: -42, z1_7: struct{}{}}, {}, T{b_0: false, p_1: &ints[5], i64_2: 8000000000, f32_3: -2.75, u32_4: 99, r_5: '世', i_6: 123456789, z1_7: struct{}{}}}
var sT = []T{T{b_0: false, p_1: &ints[5], i64_2: 8000000000, f32_3: -2.75, u32_4: 99, r_5: '世', i_6: 123456789, z1_7: struct{}{}}, T{b_0: true, p_1: &ints[2], i64_2: -9000000000, f32_3: 1.5, u32_4: 4000000000, r_5: 'x', i_6: -42, z1_7: struct{}{}}}
var aO = [2]O{O{pre: 1, in: T{b_0: true, p_1: &ints[2], i64_2: -9000000000, f32_3: 1.5, u32_4: 4000000000, r_5: 'x', i_6: -42, z1_7: struct{}{}}, post: 2}, O{pre: 3, in: T{b_0: false, p_1: &ints[5], i64_2: 8000000000, f32_3: -2.75, u32_4: 99, r_5: '世', i_6: 123456789, z1_7: struct{}{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i64_2
var pFieldO = &gO.in.i64_2
var pElem = &aT[2].i64_2
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.b_0 = false
		p.p_1 = &ints[5]
		p.i64_2 = 8000000000
		p.f32_3 = -2.75
		p.u32_4 = 99
		p.r_5 = '世'
		p.i_6 = 123456789
		p.z1_7 = struct{}{}
	} else {
		p.b_0 = true
		p.p_1 = &ints[2]
		p.i64_2 = -9000000000
		p.f32_3 = 1.5
		p.u32_4 = 4000000000
		p.r_5 = 'x'
		p.i_6 = -42
		p.z1_7 = struct{}{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("b_0", unsafe.Offsetof(t.b_0), unsafe.Sizeof(t.b_0), unsafe.Alignof(t.b_0))
	fmt.Println("p_1", unsafe.Offsetof(t.p_1), unsafe.Sizeof(t.p_1), unsafe.Alignof(t.p_1))
	fmt.Println("i64_2", unsafe.Offsetof(t.i64_2), unsafe.Sizeof(t.i64_2), unsafe.Alignof(t.i64_2))
	fmt.Println("f32_3", unsafe.Offsetof(t.f32_3), unsafe.Sizeof(t.f32_3), unsafe.Alignof(t.f32_3))
	fmt.Println("u32_4", unsafe.Offsetof(t.u32_4), unsafe.Sizeof(t.u32_4), unsafe.Alignof(t.u32_4))
	fmt.Println("r_5", unsafe.Offsetof(t.r_5), unsafe.Sizeof(t.r_5), unsafe.Alignof(t.r_5))
	fmt.Println("i_6", unsafe.Offsetof(t.i_6), unsafe.Sizeof(t.i_6), unsafe.Alignof(t.i_6))
	fmt.Println("z1_7", unsafe.Offsetof(t.z1_7), unsafe.Sizeof(t.z1_7), unsafe.Alignof(t.z1_7))
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
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_0))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_2))))
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_3))))
	fmt.Println(*(*uint32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u32_4))))
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_5))))
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_6))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.b_0, *gT.p_1, gT.i64_2, gT.f32_3, gT.u32_4, gT.r_5, gT.i_6, gT.z1_7)
	fmt.Println("directO", gO.pre, gO.in.b_0, *gO.in.p_1, gO.in.i64_2, gO.in.f32_3, gO.in.u32_4, gO.in.r_5, gO.in.i_6, gO.in.z1_7, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{b_0: true, p_1: &ints[2], i64_2: -9000000000, f32_3: 1.5, u32_4: 4000000000, r_5: 'x', i_6: -42, z1_7: struct{}{}}
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
	loc := T{b_0: false, p_1: &ints[5], i64_2: 8000000000, f32_3: -2.75, u32_4: 99, r_5: '世', i_6: 123456789, z1_7: struct{}{}}
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
	fmt.Println(cp == gT, gT == T{b_0: true, p_1: &ints[2], i64_2: -9000000000, f32_3: 1.5, u32_4: 4000000000, r_5: 'x', i_6: -42, z1_7: struct{}{}}, dT == gT)
	cp.z1_7 = struct{}{}
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
			res[i] = fmt.Sprint(v.b_0, *v.p_1, v.i64_2, v.f32_3, v.u32_4, v.r_5, v.i_6, v.z1_7, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
