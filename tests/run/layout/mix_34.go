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
	p_0   *int
	a2_1  [2]int16
	a2_2  [2]int16
	z0_3  [0]int
	any_4 any
	i64_5 int64
	up_6  uintptr
}

type O struct {
	pre byte
	T
	post int16
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, *x.p_0, x.a2_1, x.a2_2, x.z0_3, x.any_4, x.i64_5, x.up_6)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, *x.p_0, x.a2_1, x.a2_2, x.z0_3, x.any_4, x.i64_5, x.up_6)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, *x.p_0, x.a2_1, x.a2_2, x.z0_3, x.any_4, x.i64_5, x.up_6, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, *x.p_0, x.a2_1, x.a2_2, x.z0_3, x.any_4, x.i64_5, x.up_6, x.post)
}

var gT = T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, a2_2: [2]int16{-1, 2}, z0_3: [0]int{}, any_4: 42, i64_5: -9000000000, up_6: 77}
var gO = O{pre: 7, T: T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, a2_2: [2]int16{-1, 2}, z0_3: [0]int{}, any_4: 42, i64_5: -9000000000, up_6: 77}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, a2_2: [2]int16{-1, 2}, z0_3: [0]int{}, any_4: 42, i64_5: -9000000000, up_6: 77}, {}, T{p_0: &ints[5], a2_1: [2]int16{300, -300}, a2_2: [2]int16{300, -300}, z0_3: [0]int{}, any_4: "dyn", i64_5: 8000000000, up_6: 88}}
var sT = []T{T{p_0: &ints[5], a2_1: [2]int16{300, -300}, a2_2: [2]int16{300, -300}, z0_3: [0]int{}, any_4: "dyn", i64_5: 8000000000, up_6: 88}, T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, a2_2: [2]int16{-1, 2}, z0_3: [0]int{}, any_4: 42, i64_5: -9000000000, up_6: 77}}
var aO = [2]O{O{pre: 1, T: T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, a2_2: [2]int16{-1, 2}, z0_3: [0]int{}, any_4: 42, i64_5: -9000000000, up_6: 77}, post: 2}, O{pre: 3, T: T{p_0: &ints[5], a2_1: [2]int16{300, -300}, a2_2: [2]int16{300, -300}, z0_3: [0]int{}, any_4: "dyn", i64_5: 8000000000, up_6: 88}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i64_5
var pFieldO = &gO.i64_5
var pElem = &aT[2].i64_5
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.p_0 = &ints[5]
		p.a2_1 = [2]int16{300, -300}
		p.a2_2 = [2]int16{300, -300}
		p.z0_3 = [0]int{}
		p.any_4 = "dyn"
		p.i64_5 = 8000000000
		p.up_6 = 88
	} else {
		p.p_0 = &ints[2]
		p.a2_1 = [2]int16{-1, 2}
		p.a2_2 = [2]int16{-1, 2}
		p.z0_3 = [0]int{}
		p.any_4 = 42
		p.i64_5 = -9000000000
		p.up_6 = 77
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("p_0", unsafe.Offsetof(t.p_0), unsafe.Sizeof(t.p_0), unsafe.Alignof(t.p_0))
	fmt.Println("a2_1", unsafe.Offsetof(t.a2_1), unsafe.Sizeof(t.a2_1), unsafe.Alignof(t.a2_1))
	fmt.Println("a2_2", unsafe.Offsetof(t.a2_2), unsafe.Sizeof(t.a2_2), unsafe.Alignof(t.a2_2))
	fmt.Println("z0_3", unsafe.Offsetof(t.z0_3), unsafe.Sizeof(t.z0_3), unsafe.Alignof(t.z0_3))
	fmt.Println("any_4", unsafe.Offsetof(t.any_4), unsafe.Sizeof(t.any_4), unsafe.Alignof(t.any_4))
	fmt.Println("i64_5", unsafe.Offsetof(t.i64_5), unsafe.Sizeof(t.i64_5), unsafe.Alignof(t.i64_5))
	fmt.Println("up_6", unsafe.Offsetof(t.up_6), unsafe.Sizeof(t.up_6), unsafe.Alignof(t.up_6))
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
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_5))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_6))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", *gT.p_0, gT.a2_1, gT.a2_2, gT.z0_3, gT.any_4, gT.i64_5, gT.up_6)
	fmt.Println("directO", gO.pre, *gO.p_0, gO.a2_1, gO.a2_2, gO.z0_3, gO.any_4, gO.i64_5, gO.up_6, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, a2_2: [2]int16{-1, 2}, z0_3: [0]int{}, any_4: 42, i64_5: -9000000000, up_6: 77}
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
	loc := T{p_0: &ints[5], a2_1: [2]int16{300, -300}, a2_2: [2]int16{300, -300}, z0_3: [0]int{}, any_4: "dyn", i64_5: 8000000000, up_6: 88}
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
	fmt.Println(cp == gT, gT == T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, a2_2: [2]int16{-1, 2}, z0_3: [0]int{}, any_4: 42, i64_5: -9000000000, up_6: 77}, dT == gT)
	cp.up_6 = 88
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
			res[i] = fmt.Sprint(*v.p_0, v.a2_1, v.a2_2, v.z0_3, v.any_4, v.i64_5, v.up_6, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
