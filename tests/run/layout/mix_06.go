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
	r_0   rune
	p_1   *int
	i32_2 int32
	z0_3  [0]int
	r_4   rune
	any_5 any
	i_6   int
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, x.r_0, *x.p_1, x.i32_2, x.z0_3, x.r_4, x.any_5, x.i_6)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, x.r_0, *x.p_1, x.i32_2, x.z0_3, x.r_4, x.any_5, x.i_6)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, x.in.r_0, *x.in.p_1, x.in.i32_2, x.in.z0_3, x.in.r_4, x.in.any_5, x.in.i_6, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, x.in.r_0, *x.in.p_1, x.in.i32_2, x.in.z0_3, x.in.r_4, x.in.any_5, x.in.i_6, x.post)
}

var gT = T{r_0: 'x', p_1: &ints[2], i32_2: -100000, z0_3: [0]int{}, r_4: 'x', any_5: 42, i_6: -42}
var gO = O{pre: 7, in: T{r_0: 'x', p_1: &ints[2], i32_2: -100000, z0_3: [0]int{}, r_4: 'x', any_5: 42, i_6: -42}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{r_0: 'x', p_1: &ints[2], i32_2: -100000, z0_3: [0]int{}, r_4: 'x', any_5: 42, i_6: -42}, {}, T{r_0: '世', p_1: &ints[5], i32_2: 70000, z0_3: [0]int{}, r_4: '世', any_5: "dyn", i_6: 123456789}}
var sT = []T{T{r_0: '世', p_1: &ints[5], i32_2: 70000, z0_3: [0]int{}, r_4: '世', any_5: "dyn", i_6: 123456789}, T{r_0: 'x', p_1: &ints[2], i32_2: -100000, z0_3: [0]int{}, r_4: 'x', any_5: 42, i_6: -42}}
var aO = [2]O{O{pre: 1, in: T{r_0: 'x', p_1: &ints[2], i32_2: -100000, z0_3: [0]int{}, r_4: 'x', any_5: 42, i_6: -42}, post: 2}, O{pre: 3, in: T{r_0: '世', p_1: &ints[5], i32_2: 70000, z0_3: [0]int{}, r_4: '世', any_5: "dyn", i_6: 123456789}, post: 4}}
var mT = map[int]T{}
var pField = &gT.i32_2
var pFieldO = &gO.in.i32_2
var pElem = &aT[2].i32_2
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.r_0 = '世'
		p.p_1 = &ints[5]
		p.i32_2 = 70000
		p.z0_3 = [0]int{}
		p.r_4 = '世'
		p.any_5 = "dyn"
		p.i_6 = 123456789
	} else {
		p.r_0 = 'x'
		p.p_1 = &ints[2]
		p.i32_2 = -100000
		p.z0_3 = [0]int{}
		p.r_4 = 'x'
		p.any_5 = 42
		p.i_6 = -42
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("r_0", unsafe.Offsetof(t.r_0), unsafe.Sizeof(t.r_0), unsafe.Alignof(t.r_0))
	fmt.Println("p_1", unsafe.Offsetof(t.p_1), unsafe.Sizeof(t.p_1), unsafe.Alignof(t.p_1))
	fmt.Println("i32_2", unsafe.Offsetof(t.i32_2), unsafe.Sizeof(t.i32_2), unsafe.Alignof(t.i32_2))
	fmt.Println("z0_3", unsafe.Offsetof(t.z0_3), unsafe.Sizeof(t.z0_3), unsafe.Alignof(t.z0_3))
	fmt.Println("r_4", unsafe.Offsetof(t.r_4), unsafe.Sizeof(t.r_4), unsafe.Alignof(t.r_4))
	fmt.Println("any_5", unsafe.Offsetof(t.any_5), unsafe.Sizeof(t.any_5), unsafe.Alignof(t.any_5))
	fmt.Println("i_6", unsafe.Offsetof(t.i_6), unsafe.Sizeof(t.i_6), unsafe.Alignof(t.i_6))
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
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_0))))
	fmt.Println(*(*int32)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i32_2))))
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_4))))
	fmt.Println(*(*int)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i_6))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", gT.r_0, *gT.p_1, gT.i32_2, gT.z0_3, gT.r_4, gT.any_5, gT.i_6)
	fmt.Println("directO", gO.pre, gO.in.r_0, *gO.in.p_1, gO.in.i32_2, gO.in.z0_3, gO.in.r_4, gO.in.any_5, gO.in.i_6, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{r_0: 'x', p_1: &ints[2], i32_2: -100000, z0_3: [0]int{}, r_4: 'x', any_5: 42, i_6: -42}
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
	loc := T{r_0: '世', p_1: &ints[5], i32_2: 70000, z0_3: [0]int{}, r_4: '世', any_5: "dyn", i_6: 123456789}
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
	fmt.Println(cp == gT, gT == T{r_0: 'x', p_1: &ints[2], i32_2: -100000, z0_3: [0]int{}, r_4: 'x', any_5: 42, i_6: -42}, dT == gT)
	cp.i_6 = 123456789
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
			res[i] = fmt.Sprint(v.r_0, *v.p_1, v.i32_2, v.z0_3, v.r_4, v.any_5, v.i_6, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
