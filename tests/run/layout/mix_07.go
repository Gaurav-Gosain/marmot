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
	r_2   rune
	i32_3 int32
	b_4   bool
	s_5   string
	u64_6 uint64
	up_7  uintptr
	z2_8  [0]int64
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.n1_0, x.any_1, x.r_2, x.i32_3, x.b_4, x.s_5, x.u64_6, x.up_7, x.z2_8)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.n1_0, x.any_1, x.r_2, x.i32_3, x.b_4, x.s_5, x.u64_6, x.up_7, x.z2_8)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.n1_0, x.any_1, x.r_2, x.i32_3, x.b_4, x.s_5, x.u64_6, x.up_7, x.z2_8, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.n1_0, x.any_1, x.r_2, x.i32_3, x.b_4, x.s_5, x.u64_6, x.up_7, x.z2_8, x.post)
}

var gT = T{n1_0: N1{3, -7}, any_1: 42, r_2: 'x', i32_3: -100000, b_4: true, s_5: "héllo\t", u64_6: 18000000000000000000, up_7: 77, z2_8: [0]int64{}}
var gO = O{pre: 7, T: T{n1_0: N1{3, -7}, any_1: 42, r_2: 'x', i32_3: -100000, b_4: true, s_5: "héllo\t", u64_6: 18000000000000000000, up_7: 77, z2_8: [0]int64{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{n1_0: N1{3, -7}, any_1: 42, r_2: 'x', i32_3: -100000, b_4: true, s_5: "héllo\t", u64_6: 18000000000000000000, up_7: 77, z2_8: [0]int64{}}, {}, T{n1_0: N1{-3, 70000000000}, any_1: "dyn", r_2: '世', i32_3: 70000, b_4: false, s_5: "x", u64_6: 77, up_7: 88, z2_8: [0]int64{}}}
var sT = []T{T{n1_0: N1{-3, 70000000000}, any_1: "dyn", r_2: '世', i32_3: 70000, b_4: false, s_5: "x", u64_6: 77, up_7: 88, z2_8: [0]int64{}}, T{n1_0: N1{3, -7}, any_1: 42, r_2: 'x', i32_3: -100000, b_4: true, s_5: "héllo\t", u64_6: 18000000000000000000, up_7: 77, z2_8: [0]int64{}}}
var aO = [2]O{O{pre: 1, T: T{n1_0: N1{3, -7}, any_1: 42, r_2: 'x', i32_3: -100000, b_4: true, s_5: "héllo\t", u64_6: 18000000000000000000, up_7: 77, z2_8: [0]int64{}}, post: 2}, O{pre: 3, T: T{n1_0: N1{-3, 70000000000}, any_1: "dyn", r_2: '世', i32_3: 70000, b_4: false, s_5: "x", u64_6: 77, up_7: 88, z2_8: [0]int64{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i32_3
var pFieldO = &gO.i32_3
var pElem = &aT[2].i32_3
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.n1_0 = N1{-3, 70000000000}
		p.any_1 = "dyn"
		p.r_2 = '世'
		p.i32_3 = 70000
		p.b_4 = false
		p.s_5 = "x"
		p.u64_6 = 77
		p.up_7 = 88
		p.z2_8 = [0]int64{}
	} else {
		p.n1_0 = N1{3, -7}
		p.any_1 = 42
		p.r_2 = 'x'
		p.i32_3 = -100000
		p.b_4 = true
		p.s_5 = "héllo\t"
		p.u64_6 = 18000000000000000000
		p.up_7 = 77
		p.z2_8 = [0]int64{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("n1_0", unsafe.Offsetof(t.n1_0), unsafe.Sizeof(t.n1_0), unsafe.Alignof(t.n1_0))
	fmt.Println("any_1", unsafe.Offsetof(t.any_1), unsafe.Sizeof(t.any_1), unsafe.Alignof(t.any_1))
	fmt.Println("r_2", unsafe.Offsetof(t.r_2), unsafe.Sizeof(t.r_2), unsafe.Alignof(t.r_2))
	fmt.Println("i32_3", unsafe.Offsetof(t.i32_3), unsafe.Sizeof(t.i32_3), unsafe.Alignof(t.i32_3))
	fmt.Println("b_4", unsafe.Offsetof(t.b_4), unsafe.Sizeof(t.b_4), unsafe.Alignof(t.b_4))
	fmt.Println("s_5", unsafe.Offsetof(t.s_5), unsafe.Sizeof(t.s_5), unsafe.Alignof(t.s_5))
	fmt.Println("u64_6", unsafe.Offsetof(t.u64_6), unsafe.Sizeof(t.u64_6), unsafe.Alignof(t.u64_6))
	fmt.Println("up_7", unsafe.Offsetof(t.up_7), unsafe.Sizeof(t.up_7), unsafe.Alignof(t.up_7))
	fmt.Println("z2_8", unsafe.Offsetof(t.z2_8), unsafe.Sizeof(t.z2_8), unsafe.Alignof(t.z2_8))
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
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_2))))
	fmt.Println(*(*int32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i32_3))))
	fmt.Println(*(*bool)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b_4))))
	fmt.Println(*(*string)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.s_5))))
	fmt.Println(*(*uint64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u64_6))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_7))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.n1_0, gT.any_1, gT.r_2, gT.i32_3, gT.b_4, gT.s_5, gT.u64_6, gT.up_7, gT.z2_8)
	fmt.Println("directO", gO.pre, gO.n1_0, gO.any_1, gO.r_2, gO.i32_3, gO.b_4, gO.s_5, gO.u64_6, gO.up_7, gO.z2_8, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{n1_0: N1{3, -7}, any_1: 42, r_2: 'x', i32_3: -100000, b_4: true, s_5: "héllo\t", u64_6: 18000000000000000000, up_7: 77, z2_8: [0]int64{}}
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
	loc := T{n1_0: N1{-3, 70000000000}, any_1: "dyn", r_2: '世', i32_3: 70000, b_4: false, s_5: "x", u64_6: 77, up_7: 88, z2_8: [0]int64{}}
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
	fmt.Println(cp == gT, gT == T{n1_0: N1{3, -7}, any_1: 42, r_2: 'x', i32_3: -100000, b_4: true, s_5: "héllo\t", u64_6: 18000000000000000000, up_7: 77, z2_8: [0]int64{}}, dT == gT)
	cp.z2_8 = [0]int64{}
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
			res[i] = fmt.Sprint(v.n1_0, v.any_1, v.r_2, v.i32_3, v.b_4, v.s_5, v.u64_6, v.up_7, v.z2_8, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
