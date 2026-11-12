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
	s_0   string
	i64_1 int64
	z0_2  [0]int
	f32_3 float32
	i64_4 int64
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.s_0, x.i64_1, x.z0_2, x.f32_3, x.i64_4)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.s_0, x.i64_1, x.z0_2, x.f32_3, x.i64_4)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.s_0, x.in.i64_1, x.in.z0_2, x.in.f32_3, x.in.i64_4, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.s_0, x.in.i64_1, x.in.z0_2, x.in.f32_3, x.in.i64_4, x.post)
}

var gT = T{s_0: "héllo\t", i64_1: -9000000000, z0_2: [0]int{}, f32_3: 1.5, i64_4: -9000000000}
var gO = O{pre: 7, in: T{s_0: "héllo\t", i64_1: -9000000000, z0_2: [0]int{}, f32_3: 1.5, i64_4: -9000000000}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{s_0: "héllo\t", i64_1: -9000000000, z0_2: [0]int{}, f32_3: 1.5, i64_4: -9000000000}, {}, T{s_0: "x", i64_1: 8000000000, z0_2: [0]int{}, f32_3: -2.75, i64_4: 8000000000}}
var sT = []T{T{s_0: "x", i64_1: 8000000000, z0_2: [0]int{}, f32_3: -2.75, i64_4: 8000000000}, T{s_0: "héllo\t", i64_1: -9000000000, z0_2: [0]int{}, f32_3: 1.5, i64_4: -9000000000}}
var aO = [2]O{O{pre: 1, in: T{s_0: "héllo\t", i64_1: -9000000000, z0_2: [0]int{}, f32_3: 1.5, i64_4: -9000000000}, post: 2}, O{pre: 3, in: T{s_0: "x", i64_1: 8000000000, z0_2: [0]int{}, f32_3: -2.75, i64_4: 8000000000}, post: 4}}
var mT = map[int]T{}
var pField = &gT.s_0
var pFieldO = &gO.in.s_0
var pElem = &aT[2].s_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.s_0 = "x"
		p.i64_1 = 8000000000
		p.z0_2 = [0]int{}
		p.f32_3 = -2.75
		p.i64_4 = 8000000000
	} else {
		p.s_0 = "héllo\t"
		p.i64_1 = -9000000000
		p.z0_2 = [0]int{}
		p.f32_3 = 1.5
		p.i64_4 = -9000000000
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("s_0", unsafe.Offsetof(t.s_0), unsafe.Sizeof(t.s_0), unsafe.Alignof(t.s_0))
	fmt.Println("i64_1", unsafe.Offsetof(t.i64_1), unsafe.Sizeof(t.i64_1), unsafe.Alignof(t.i64_1))
	fmt.Println("z0_2", unsafe.Offsetof(t.z0_2), unsafe.Sizeof(t.z0_2), unsafe.Alignof(t.z0_2))
	fmt.Println("f32_3", unsafe.Offsetof(t.f32_3), unsafe.Sizeof(t.f32_3), unsafe.Alignof(t.f32_3))
	fmt.Println("i64_4", unsafe.Offsetof(t.i64_4), unsafe.Sizeof(t.i64_4), unsafe.Alignof(t.i64_4))
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
	fmt.Println(*(*string)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.s_0))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_1))))
	fmt.Println(*(*float32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.f32_3))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_4))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.s_0, gT.i64_1, gT.z0_2, gT.f32_3, gT.i64_4)
	fmt.Println("directO", gO.pre, gO.in.s_0, gO.in.i64_1, gO.in.z0_2, gO.in.f32_3, gO.in.i64_4, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{s_0: "héllo\t", i64_1: -9000000000, z0_2: [0]int{}, f32_3: 1.5, i64_4: -9000000000}
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
	loc := T{s_0: "x", i64_1: 8000000000, z0_2: [0]int{}, f32_3: -2.75, i64_4: 8000000000}
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
	fmt.Println(cp == gT, gT == T{s_0: "héllo\t", i64_1: -9000000000, z0_2: [0]int{}, f32_3: 1.5, i64_4: -9000000000}, dT == gT)
	cp.i64_4 = 8000000000
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
			res[i] = fmt.Sprint(v.s_0, v.i64_1, v.z0_2, v.f32_3, v.i64_4, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
