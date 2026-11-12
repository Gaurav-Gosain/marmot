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
	up_0  uintptr
	u16_1 uint16
	i8_2  int8
	up_3  uintptr
	z2_4  [0]int64
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.up_0, x.u16_1, x.i8_2, x.up_3, x.z2_4)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.up_0, x.u16_1, x.i8_2, x.up_3, x.z2_4)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.up_0, x.in.u16_1, x.in.i8_2, x.in.up_3, x.in.z2_4, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.up_0, x.in.u16_1, x.in.i8_2, x.in.up_3, x.in.z2_4, x.post)
}

var gT = T{up_0: 77, u16_1: 60000, i8_2: -5, up_3: 77, z2_4: [0]int64{}}
var gO = O{pre: 7, in: T{up_0: 77, u16_1: 60000, i8_2: -5, up_3: 77, z2_4: [0]int64{}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{up_0: 77, u16_1: 60000, i8_2: -5, up_3: 77, z2_4: [0]int64{}}, {}, T{up_0: 88, u16_1: 513, i8_2: 7, up_3: 88, z2_4: [0]int64{}}}
var sT = []T{T{up_0: 88, u16_1: 513, i8_2: 7, up_3: 88, z2_4: [0]int64{}}, T{up_0: 77, u16_1: 60000, i8_2: -5, up_3: 77, z2_4: [0]int64{}}}
var aO = [2]O{O{pre: 1, in: T{up_0: 77, u16_1: 60000, i8_2: -5, up_3: 77, z2_4: [0]int64{}}, post: 2}, O{pre: 3, in: T{up_0: 88, u16_1: 513, i8_2: 7, up_3: 88, z2_4: [0]int64{}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.up_0
var pFieldO = &gO.in.up_0
var pElem = &aT[2].up_0
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.up_0 = 88
		p.u16_1 = 513
		p.i8_2 = 7
		p.up_3 = 88
		p.z2_4 = [0]int64{}
	} else {
		p.up_0 = 77
		p.u16_1 = 60000
		p.i8_2 = -5
		p.up_3 = 77
		p.z2_4 = [0]int64{}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("up_0", unsafe.Offsetof(t.up_0), unsafe.Sizeof(t.up_0), unsafe.Alignof(t.up_0))
	fmt.Println("u16_1", unsafe.Offsetof(t.u16_1), unsafe.Sizeof(t.u16_1), unsafe.Alignof(t.u16_1))
	fmt.Println("i8_2", unsafe.Offsetof(t.i8_2), unsafe.Sizeof(t.i8_2), unsafe.Alignof(t.i8_2))
	fmt.Println("up_3", unsafe.Offsetof(t.up_3), unsafe.Sizeof(t.up_3), unsafe.Alignof(t.up_3))
	fmt.Println("z2_4", unsafe.Offsetof(t.z2_4), unsafe.Sizeof(t.z2_4), unsafe.Alignof(t.z2_4))
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
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_0))))
	fmt.Println(*(*uint16)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u16_1))))
	fmt.Println(*(*int8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i8_2))))
	fmt.Println(*(*uintptr)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.up_3))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.up_0, gT.u16_1, gT.i8_2, gT.up_3, gT.z2_4)
	fmt.Println("directO", gO.pre, gO.in.up_0, gO.in.u16_1, gO.in.i8_2, gO.in.up_3, gO.in.z2_4, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{up_0: 77, u16_1: 60000, i8_2: -5, up_3: 77, z2_4: [0]int64{}}
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
	loc := T{up_0: 88, u16_1: 513, i8_2: 7, up_3: 88, z2_4: [0]int64{}}
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
	fmt.Println(cp == gT, gT == T{up_0: 77, u16_1: 60000, i8_2: -5, up_3: 77, z2_4: [0]int64{}}, dT == gT)
	cp.z2_4 = [0]int64{}
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
			res[i] = fmt.Sprint(v.up_0, v.u16_1, v.i8_2, v.up_3, v.z2_4, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
