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
	r_2   rune
	r_3   rune
	z2_4  [0]int64
	u8_5  uint8
	i64_6 int64
	sl_7  []int
	s_8   string
	n2_9  N2
}

type O struct {
	pre  byte
	in   T
	post int16
	z    struct{}
}

//go:noinline
func showT(tag string, x T) {
	fmt.Println(tag, *x.p_0, x.a2_1, x.r_2, x.r_3, x.z2_4, x.u8_5, x.i64_6, x.sl_7, x.s_8, x.n2_9)
}

//go:noinline
func showTP(tag string, x *T) {
	fmt.Println(tag, *x.p_0, x.a2_1, x.r_2, x.r_3, x.z2_4, x.u8_5, x.i64_6, x.sl_7, x.s_8, x.n2_9)
}

//go:noinline
func showO(tag string, x O) {
	fmt.Println(tag, x.pre, *x.in.p_0, x.in.a2_1, x.in.r_2, x.in.r_3, x.in.z2_4, x.in.u8_5, x.in.i64_6, x.in.sl_7, x.in.s_8, x.in.n2_9, x.post)
}

//go:noinline
func showOP(tag string, x *O) {
	fmt.Println(tag, x.pre, *x.in.p_0, x.in.a2_1, x.in.r_2, x.in.r_3, x.in.z2_4, x.in.u8_5, x.in.i64_6, x.in.sl_7, x.in.s_8, x.in.n2_9, x.post)
}

var gT = T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, r_2: 'x', r_3: 'x', z2_4: [0]int64{}, u8_5: 200, i64_6: -9000000000, sl_7: []int{1, 2, 3}, s_8: "héllo\t", n2_9: N2{true, [3]byte{9, 8, 7}, 2.5}}
var gO = O{pre: 7, in: T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, r_2: 'x', r_3: 'x', z2_4: [0]int64{}, u8_5: 200, i64_6: -9000000000, sl_7: []int{1, 2, 3}, s_8: "héllo\t", n2_9: N2{true, [3]byte{9, 8, 7}, 2.5}}, post: -9}
var dT, dO = T{}, O{}
var aT = [3]T{T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, r_2: 'x', r_3: 'x', z2_4: [0]int64{}, u8_5: 200, i64_6: -9000000000, sl_7: []int{1, 2, 3}, s_8: "héllo\t", n2_9: N2{true, [3]byte{9, 8, 7}, 2.5}}, {}, T{p_0: &ints[5], a2_1: [2]int16{300, -300}, r_2: '世', r_3: '世', z2_4: [0]int64{}, u8_5: 13, i64_6: 8000000000, sl_7: []int{9}, s_8: "x", n2_9: N2{false, [3]byte{1, 1, 1}, -0.5}}}
var sT = []T{T{p_0: &ints[5], a2_1: [2]int16{300, -300}, r_2: '世', r_3: '世', z2_4: [0]int64{}, u8_5: 13, i64_6: 8000000000, sl_7: []int{9}, s_8: "x", n2_9: N2{false, [3]byte{1, 1, 1}, -0.5}}, T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, r_2: 'x', r_3: 'x', z2_4: [0]int64{}, u8_5: 200, i64_6: -9000000000, sl_7: []int{1, 2, 3}, s_8: "héllo\t", n2_9: N2{true, [3]byte{9, 8, 7}, 2.5}}}
var aO = [2]O{O{pre: 1, in: T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, r_2: 'x', r_3: 'x', z2_4: [0]int64{}, u8_5: 200, i64_6: -9000000000, sl_7: []int{1, 2, 3}, s_8: "héllo\t", n2_9: N2{true, [3]byte{9, 8, 7}, 2.5}}, post: 2}, O{pre: 3, in: T{p_0: &ints[5], a2_1: [2]int16{300, -300}, r_2: '世', r_3: '世', z2_4: [0]int64{}, u8_5: 13, i64_6: 8000000000, sl_7: []int{9}, s_8: "x", n2_9: N2{false, [3]byte{1, 1, 1}, -0.5}}, post: 4}}
var mT = map[int]T{}
var pField = &gT.u8_5
var pFieldO = &gO.in.u8_5
var pElem = &aT[2].u8_5
var pBlank = &aO[1].post

//go:noinline
func setDyn(p *T, alt bool) {
	if alt {
		p.p_0 = &ints[5]
		p.a2_1 = [2]int16{300, -300}
		p.r_2 = '世'
		p.r_3 = '世'
		p.z2_4 = [0]int64{}
		p.u8_5 = 13
		p.i64_6 = 8000000000
		p.sl_7 = []int{9}
		p.s_8 = "x"
		p.n2_9 = N2{false, [3]byte{1, 1, 1}, -0.5}
	} else {
		p.p_0 = &ints[2]
		p.a2_1 = [2]int16{-1, 2}
		p.r_2 = 'x'
		p.r_3 = 'x'
		p.z2_4 = [0]int64{}
		p.u8_5 = 200
		p.i64_6 = -9000000000
		p.sl_7 = []int{1, 2, 3}
		p.s_8 = "héllo\t"
		p.n2_9 = N2{true, [3]byte{9, 8, 7}, 2.5}
	}
}

func sizes() {
	var t T
	var o O
	fmt.Println(unsafe.Sizeof(t), unsafe.Alignof(t), unsafe.Sizeof(o), unsafe.Alignof(o))
	fmt.Println("p_0", unsafe.Offsetof(t.p_0), unsafe.Sizeof(t.p_0), unsafe.Alignof(t.p_0))
	fmt.Println("a2_1", unsafe.Offsetof(t.a2_1), unsafe.Sizeof(t.a2_1), unsafe.Alignof(t.a2_1))
	fmt.Println("r_2", unsafe.Offsetof(t.r_2), unsafe.Sizeof(t.r_2), unsafe.Alignof(t.r_2))
	fmt.Println("r_3", unsafe.Offsetof(t.r_3), unsafe.Sizeof(t.r_3), unsafe.Alignof(t.r_3))
	fmt.Println("z2_4", unsafe.Offsetof(t.z2_4), unsafe.Sizeof(t.z2_4), unsafe.Alignof(t.z2_4))
	fmt.Println("u8_5", unsafe.Offsetof(t.u8_5), unsafe.Sizeof(t.u8_5), unsafe.Alignof(t.u8_5))
	fmt.Println("i64_6", unsafe.Offsetof(t.i64_6), unsafe.Sizeof(t.i64_6), unsafe.Alignof(t.i64_6))
	fmt.Println("sl_7", unsafe.Offsetof(t.sl_7), unsafe.Sizeof(t.sl_7), unsafe.Alignof(t.sl_7))
	fmt.Println("s_8", unsafe.Offsetof(t.s_8), unsafe.Sizeof(t.s_8), unsafe.Alignof(t.s_8))
	fmt.Println("n2_9", unsafe.Offsetof(t.n2_9), unsafe.Sizeof(t.n2_9), unsafe.Alignof(t.n2_9))
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
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_2))))
	fmt.Println(*(*rune)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.r_3))))
	fmt.Println(*(*uint8)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.u8_5))))
	fmt.Println(*(*int64)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.i64_6))))
	fmt.Println(*(*string)(unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.s_8))))
}

func main() {
	sizes()
	showT("static", gT)
	showTP("static ptr", &gT)
	showO("static O", gO)
	showOP("static O ptr", &gO)
	fmt.Println("direct", *gT.p_0, gT.a2_1, gT.r_2, gT.r_3, gT.z2_4, gT.u8_5, gT.i64_6, gT.sl_7, gT.s_8, gT.n2_9)
	fmt.Println("directO", gO.pre, *gO.in.p_0, gO.in.a2_1, gO.in.r_2, gO.in.r_3, gO.in.z2_4, gO.in.u8_5, gO.in.i64_6, gO.in.sl_7, gO.in.s_8, gO.in.n2_9, gO.post)
	fmt.Println(*pField, *pFieldO, *pElem, *pBlank)
	setDyn(&dT, false)
	showT("dyn", dT)
	setDyn(&dT, true)
	showTP("dyn alt", &dT)
	dT = T{p_0: &ints[2], a2_1: [2]int16{-1, 2}, r_2: 'x', r_3: 'x', z2_4: [0]int64{}, u8_5: 200, i64_6: -9000000000, sl_7: []int{1, 2, 3}, s_8: "héllo\t", n2_9: N2{true, [3]byte{9, 8, 7}, 2.5}}
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
	loc := T{p_0: &ints[5], a2_1: [2]int16{300, -300}, r_2: '世', r_3: '世', z2_4: [0]int64{}, u8_5: 13, i64_6: 8000000000, sl_7: []int{9}, s_8: "x", n2_9: N2{false, [3]byte{1, 1, 1}, -0.5}}
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
			res[i] = fmt.Sprint(*v.p_0, v.a2_1, v.r_2, v.r_3, v.z2_4, v.u8_5, v.i64_6, v.sl_7, v.s_8, v.n2_9, o.post)
		}(sT[i%2], gO)
	}
	wg.Wait()
	fmt.Println(res)
	done := make(chan struct{})
	go func() { showTP("go closure", &aT[2]); close(done) }()
	<-done
}
