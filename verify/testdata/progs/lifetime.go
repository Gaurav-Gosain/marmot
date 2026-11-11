// lifetime: aliases that in-place reuse (rt_free after append growth), region inference and
// frame regions must not miss. Each case keeps a second route to an object and checks it
// after the transform could have freed or recycled the object. Run with MARMOT_REUSE=1,
// regions on, MARMOT_GCSTRESS=1 and the poisoning debug mode.
package main

import (
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"unsafe"
)

type T struct {
	p, q *T
	v    int
}

var sink interface{}

//go:noinline
func churn(c int) {
	for i := 0; i < 64; i++ {
		s := make([]int, c)
		for j := range s {
			s[j] = -7
		}
		x := &T{v: -7}
		sink = x
		_ = s
	}
	sink = nil
}

// ---- in-place reuse: a second route to the old backing array ----

//go:noinline
func viaIface(c int) int {
	s := make([]int, 1, c)
	s[0] = 11
	var keep interface{} = s // the slice escapes into an interface
	s = append(s, 1, 2, 3)
	churn(c)
	return keep.([]int)[0]
}

//go:noinline
func viaClosure(c int) int {
	s := make([]int, 1, c)
	s[0] = 12
	get := func() int { return s[0] } // captures s (by reference)
	t := append(s, 4, 5, 6)
	churn(c)
	return get() + 0*len(t)
}

//go:noinline
func viaSubslice(c int) int {
	s := make([]int, 2, c)
	s[1] = 13
	sub := s[1:2] // shares the array
	s = append(s, 7, 8, 9)
	churn(c)
	return sub[0]
}

//go:noinline
func viaSliceOfSlices(c int) int {
	s := make([]int, 1, c)
	s[0] = 14
	rows := [][]int{s}
	s = append(s, 1, 2, 3)
	churn(c)
	return rows[0][0]
}

//go:noinline
func viaMap(c int) int {
	s := make([]int, 1, c)
	s[0] = 15
	m := map[int][]int{1: s}
	s = append(s, 1, 2, 3)
	churn(c)
	return m[1][0]
}

//go:noinline
func viaChannel(c int) int {
	s := make([]int, 1, c)
	s[0] = 16
	ch := make(chan []int, 1)
	ch <- s
	s = append(s, 1, 2, 3)
	churn(c)
	return (<-ch)[0]
}

//go:noinline
func viaGoroutine(c int) int {
	s := make([]int, 1, c)
	s[0] = 17
	var wg sync.WaitGroup
	got := 0
	wg.Add(1)
	start := make(chan bool)
	go func(t []int) {
		<-start
		got = t[0]
		wg.Done()
	}(s)
	s = append(s, 1, 2, 3)
	churn(c)
	close(start)
	wg.Wait()
	return got
}

//go:noinline
func viaUnsafe(c int) int {
	s := make([]int, 1, c)
	s[0] = 18
	p := unsafe.Pointer(&s[0])
	s = append(s, 1, 2, 3)
	churn(c)
	return *(*int)(p)
}

//go:noinline
func viaReflect(c int) int {
	s := make([]int, 1, c)
	s[0] = 19
	rv := reflect.ValueOf(s)
	s = append(s, 1, 2, 3)
	churn(c)
	return int(rv.Index(0).Int())
}

//go:noinline
func viaDeferRecover(c int) (r int) {
	s := make([]int, 1, c)
	s[0] = 20
	defer func() {
		recover()
		r = s[0]
	}()
	s = append(s, 1, 2, 3) // s is redefined: the deferred closure sees the new array
	s[0] = 21
	churn(c)
	panic("x")
}

//go:noinline
func viaEarlierValue(c int) int {
	v := make([]int, 1, c)
	v[0] = 22
	a := append(v, 1)
	b := append(a, 2, 3, 4)
	churn(c)
	return v[0] + a[0] + 0*len(b)
}

// ---- regions: a second route into a callee's fresh graph ----

//go:noinline
func mk(h *T, v int) *T {
	n := &T{v: v}
	m := &T{v: v + 1}
	n.p = m
	n.q = h
	return n
}

//go:noinline
func regionCompareOnly(h *T) bool {
	x := mk(h, 30)
	return x != nil && x.p.v == 31
}

//go:noinline
func regionThenEscape(h *T) *T {
	x := mk(h, 32)
	return x.p // escapes: must not be region allocated
}

//go:noinline
func regionInIface(h *T) interface{} {
	x := mk(h, 34)
	return x
}

var fin int

//go:noinline
func regionFinalizer(h *T) int {
	x := mk(h, 36)
	runtime.SetFinalizer(x.p, func(*T) { fin++ })
	return x.v
}

// ---- frame regions ----

type list struct{ head *T }

//go:noinline
func pushFront(l *list, v int) {
	n := &T{v: v}
	n.p = l.head
	l.head = n
}

var keepT *T

//go:noinline
func frameLeak() int {
	var l list
	pushFront(&l, 40)
	pushFront(&l, 41)
	keepT = l.head // escapes the frame object
	return l.head.v
}

//go:noinline
func frameLocal() int {
	var l list
	for i := 0; i < 10; i++ {
		pushFront(&l, i)
	}
	s := 0
	for p := l.head; p != nil; p = p.p {
		s += p.v
	}
	return s
}

func main() {
	c := 4
	fmt.Println(viaIface(c), viaClosure(c), viaSubslice(c), viaSliceOfSlices(c), viaMap(c))
	fmt.Println(viaChannel(c), viaGoroutine(c), viaUnsafe(c), viaReflect(c), viaDeferRecover(c), viaEarlierValue(c))
	h := &T{v: 1}
	fmt.Println(regionCompareOnly(h), regionThenEscape(h).v, regionInIface(h).(*T).p.v, regionFinalizer(h))
	fmt.Println(frameLeak(), frameLocal())
	churn(c)
	runtime.GC()
	fmt.Println(keepT.v, keepT.p.v, h.v, h.p == nil)
}
