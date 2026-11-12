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
	any_1 any
	a2_2  [2]int16
	a3_3  [3]byte
	i64_4 int64
	p_5   *int
	p_6   *int
	up_7  uintptr
	r_8   rune
	i8_9  int8
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.z0_0, x.any_1, x.a2_2, x.a3_3, x.i64_4, *x.p_5, *x.p_6, x.up_7, x.r_8, x.i8_9)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.z0_0, x.any_1, x.a2_2, x.a3_3, x.i64_4, *x.p_5, *x.p_6, x.up_7, x.r_8, x.i8_9)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.z0_0, x.in.any_1, x.in.a2_2, x.in.a3_3, x.in.i64_4, *x.in.p_5, *x.in.p_6, x.in.up_7, x.in.r_8, x.in.i8_9, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.z0_0, x.in.any_1, x.in.a2_2, x.in.a3_3, x.in.i64_4, *x.in.p_5, *x.in.p_6, x.in.up_7, x.in.r_8, x.in.i8_9, x.post)
}

var gT = T{z0_0: [0]int{}, any_1: 42, a2_2: [2]int16{-1, 2}, a3_3: [3]byte{1, 2, 3}, i64_4: -9000000000, p_5: &ints[2], p_6: &ints[2], up_7: 77, r_8: 'x', i8_9: -5}
var gO = O{pre: 7, in: T{z0_0: [0]int{}, any_1: 42, a2_2: [2]int16{-1, 2}, a3_3: [3]byte{1, 2, 3}, i64_4: -9000000000, p_5: &ints[2], p_6: &ints[2], up_7: 77, r_8: 'x', i8_9: -5}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{z0_0: [0]int{}, any_1: 42, a2_2: [2]int16{-1, 2}, a3_3: [3]byte{1, 2, 3}, i64_4: -9000000000, p_5: &ints[2], p_6: &ints[2], up_7: 77, r_8: 'x', i8_9: -5}, {}, T{z0_0: [0]int{}, any_1: "dyn", a2_2: [2]int16{300, -300}, a3_3: [3]byte{7, 8, 9}, i64_4: 8000000000, p_5: &ints[5], p_6: &ints[5], up_7: 88, r_8: '世', i8_9: 7}}
var sT = []T{T{z0_0: [0]int{}, any_1: "dyn", a2_2: [2]int16{300, -300}, a3_3: [3]byte{7, 8, 9}, i64_4: 8000000000, p_5: &ints[5], p_6: &ints[5], up_7: 88, r_8: '世', i8_9: 7}, T{z0_0: [0]int{}, any_1: 42, a2_2: [2]int16{-1, 2}, a3_3: [3]byte{1, 2, 3}, i64_4: -9000000000, p_5: &ints[2], p_6: &ints[2], up_7: 77, r_8: 'x', i8_9: -5}}
var aO = [2]O{O{pre: 1, in: T{z0_0: [0]int{}, any_1: 42, a2_2: [2]int16{-1, 2}, a3_3: [3]byte{1, 2, 3}, i64_4: -9000000000, p_5: &ints[2], p_6: &ints[2], up_7: 77, r_8: 'x', i8_9: -5}, post: 2}, O{pre: 3, in: T{z0_0: [0]int{}, any_1: "dyn", a2_2: [2]int16{300, -300}, a3_3: [3]byte{7, 8, 9}, i64_4: 8000000000, p_5: &ints[5], p_6: &ints[5], up_7: 88, r_8: '世', i8_9: 7}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i64_4
var pFieldO = &gO.in.i64_4
var pElem = &aT[2].i64_4
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.z0_0 = [0]int{}
		p.any_1 = "dyn"
		p.a2_2 = [2]int16{300, -300}
		p.a3_3 = [3]byte{7, 8, 9}
		p.i64_4 = 8000000000
		p.p_5 = &ints[5]
		p.p_6 = &ints[5]
		p.up_7 = 88
		p.r_8 = '世'
		p.i8_9 = 7
	} else {
		p.z0_0 = [0]int{}
		p.any_1 = 42
		p.a2_2 = [2]int16{-1, 2}
		p.a3_3 = [3]byte{1, 2, 3}
		p.i64_4 = -9000000000
		p.p_5 = &ints[2]
		p.p_6 = &ints[2]
		p.up_7 = 77
		p.r_8 = 'x'
		p.i8_9 = -5
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("z0_0", unsafe.Offsetof(t.z0_0), unsafe.Sizeof(t.z0_0), unsafe.Alignof(t.z0_0))
	fmt.Println("any_1", unsafe.Offsetof(t.any_1), unsafe.Sizeof(t.any_1), unsafe.Alignof(t.any_1))
	fmt.Println("a2_2", unsafe.Offsetof(t.a2_2), unsafe.Sizeof(t.a2_2), unsafe.Alignof(t.a2_2))
	fmt.Println("a3_3", unsafe.Offsetof(t.a3_3), unsafe.Sizeof(t.a3_3), unsafe.Alignof(t.a3_3))
	fmt.Println("i64_4", unsafe.Offsetof(t.i64_4), unsafe.Sizeof(t.i64_4), unsafe.Alignof(t.i64_4))
	fmt.Println("p_5", unsafe.Offsetof(t.p_5), unsafe.Sizeof(t.p_5), unsafe.Alignof(t.p_5))
	fmt.Println("p_6", unsafe.Offsetof(t.p_6), unsafe.Sizeof(t.p_6), unsafe.Alignof(t.p_6))
	fmt.Println("up_7", unsafe.Offsetof(t.up_7), unsafe.Sizeof(t.up_7), unsafe.Alignof(t.up_7))
	fmt.Println("r_8", unsafe.Offsetof(t.r_8), unsafe.Sizeof(t.r_8), unsafe.Alignof(t.r_8))
	fmt.Println("i8_9", unsafe.Offsetof(t.i8_9), unsafe.Sizeof(t.i8_9), unsafe.Alignof(t.i8_9))
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
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_4))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_7))))
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_8))))
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_9))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.z0_0, gT.any_1, gT.a2_2, gT.a3_3, gT.i64_4, *gT.p_5, *gT.p_6, gT.up_7, gT.r_8, gT.i8_9)
	fmt.Println("directO", gO.pre, gO.in.z0_0, gO.in.any_1, gO.in.a2_2, gO.in.a3_3, gO.in.i64_4, *gO.in.p_5, *gO.in.p_6, gO.in.up_7, gO.in.r_8, gO.in.i8_9, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{z0_0: [0]int{}, any_1: 42, a2_2: [2]int16{-1, 2}, a3_3: [3]byte{1, 2, 3}, i64_4: -9000000000, p_5: &ints[2], p_6: &ints[2], up_7: 77, r_8: 'x', i8_9: -5}
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
	loc := T{z0_0: [0]int{}, any_1: "dyn", a2_2: [2]int16{300, -300}, a3_3: [3]byte{7, 8, 9}, i64_4: 8000000000, p_5: &ints[5], p_6: &ints[5], up_7: 88, r_8: '世', i8_9: 7}
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
	fmt.Println(cp == gT, gT == T{z0_0: [0]int{}, any_1: 42, a2_2: [2]int16{-1, 2}, a3_3: [3]byte{1, 2, 3}, i64_4: -9000000000, p_5: &ints[2], p_6: &ints[2], up_7: 77, r_8: 'x', i8_9: -5}, dT == gT)
	cp.i8_9 = 7
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
			res[i] = fmt.Sprint(v.z0_0, v.any_1, v.a2_2, v.a3_3, v.i64_4, *v.p_5, *v.p_6, v.up_7, v.r_8, v.i8_9, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
