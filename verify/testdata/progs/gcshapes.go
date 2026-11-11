// gcshapes: every type shape that holds a pointer, with the only reference to a fresh object
// in the field under test, checked after forced collections. A word the compiler marks as a
// non-pointer (or an object allocated noscan) lets the collector free the object; run with
// MARMOT_GCSTRESS=1 MARMOT_POISON=1 to make that visible. Each check prints the value it
// reads through the field; the output must equal stock Go's.
package main

import (
	"fmt"
	"runtime"
	"strings"
	"unsafe"
)

type val struct{ n int }

//go:noinline
func mk(n int) *val { return &val{n} }

//go:noinline
func mks(n int) string { return strings.Repeat(fmt.Sprint(n%10), 20) } // heap string

func gc() {
	for i := 0; i < 3; i++ {
		runtime.GC()
		churn = make([]*val, 512)
		for j := range churn {
			churn[j] = &val{-1}
		}
	}
	churn = nil
}

var churn []*val

type padded struct {
	a bool
	p *val
	b int8
	q *val
	c [3]byte
}

type nested struct {
	x  int
	in struct {
		y int
		p *val
		z [2]struct {
			f float64
			q *val
		}
	}
}

type bigPrefix struct {
	pad [100]int // 800 bytes of non-pointers before the pointer
	p   *val
}

type huge struct { // over 32 KB: large object path
	pad [5000]int
	p   *val
	tail [100]int
	q   *val
}

type withIface struct {
	k int
	i interface{}
}

type withFunc struct {
	f func() int
}

type withMapChan struct {
	m map[int]*val
	c chan *val
}

type withSliceStr struct {
	s   []*val
	str string
}

type zeroTail struct {
	p *val
	z [0]int
}

type arrOfStructOnePtrAtEnd [8]struct {
	a, b, c int
	p       *val
}

type unsafeHolder struct {
	u unsafe.Pointer
}

type genericBox[T any] struct {
	pad int
	v   T
}

//go:noinline
func build() []interface{} {
	var out []interface{}
	out = append(out, &padded{p: mk(1), q: mk(2)})
	n := &nested{}
	n.in.p = mk(3)
	n.in.z[1].q = mk(4)
	out = append(out, n)
	out = append(out, &bigPrefix{p: mk(5)})
	h := &huge{}
	h.p, h.q = mk(6), mk(7)
	out = append(out, h)
	out = append(out, &withIface{i: mk(8)})
	out = append(out, &withIface{i: padded{p: mk(9)}}) // boxed struct value inside an interface
	v10 := mk(10)
	out = append(out, &withFunc{f: func() int { return v10.n }})
	m := map[int]*val{1: mk(11)}
	c := make(chan *val, 4)
	c <- mk(12)
	out = append(out, &withMapChan{m: m, c: c})
	out = append(out, &withSliceStr{s: []*val{mk(13)}, str: mks(14)})
	out = append(out, &zeroTail{p: mk(15)})
	var a arrOfStructOnePtrAtEnd
	a[7].p = mk(16)
	out = append(out, &a)
	out = append(out, &unsafeHolder{u: unsafe.Pointer(mk(17))})
	out = append(out, &genericBox[*val]{v: mk(18)})
	out = append(out, &genericBox[string]{v: mks(19)})
	out = append(out, &genericBox[[2]interface{}]{v: [2]interface{}{1, mk(20)}})
	// map keys holding pointers, map values that are structs with pointers
	mk2 := map[*val]int{mk(21): 21}
	out = append(out, mk2)
	mv := map[string]padded{"a": {q: mk(22)}}
	out = append(out, mv)
	// channel buffer of structs with pointers
	cs := make(chan padded, 2)
	cs <- padded{p: mk(23)}
	out = append(out, cs)
	// slice grown by append, pointer only in the grown part
	var gs []*val
	for i := 0; i < 100; i++ {
		gs = append(gs, nil)
	}
	gs = append(gs, mk(24))
	out = append(out, gs)
	// uintptr is not a pointer: Go only allows the round trip within one expression
	p25 := mk(25)
	out = append(out, (*val)(unsafe.Pointer(uintptr(unsafe.Pointer(p25)))))
	return out
}

func main() {
	out := build()
	gc()
	var r []int
	r = append(r, out[0].(*padded).p.n, out[0].(*padded).q.n)
	n := out[1].(*nested)
	r = append(r, n.in.p.n, n.in.z[1].q.n)
	r = append(r, out[2].(*bigPrefix).p.n)
	h := out[3].(*huge)
	r = append(r, h.p.n, h.q.n)
	r = append(r, out[4].(*withIface).i.(*val).n)
	r = append(r, out[5].(*withIface).i.(padded).p.n)
	r = append(r, out[6].(*withFunc).f())
	mc := out[7].(*withMapChan)
	r = append(r, mc.m[1].n, (<-mc.c).n)
	ss := out[8].(*withSliceStr)
	r = append(r, ss.s[0].n, int(ss.str[19]-'0'))
	r = append(r, out[9].(*zeroTail).p.n)
	r = append(r, out[10].(*arrOfStructOnePtrAtEnd)[7].p.n)
	r = append(r, (*val)(out[11].(*unsafeHolder).u).n)
	r = append(r, out[12].(*genericBox[*val]).v.n)
	r = append(r, int(out[13].(*genericBox[string]).v[0]-'0'))
	r = append(r, out[14].(*genericBox[[2]interface{}]).v[1].(*val).n)
	for k, v := range out[15].(map[*val]int) {
		r = append(r, k.n, v)
	}
	r = append(r, out[16].(map[string]padded)["a"].q.n)
	r = append(r, (<-out[17].(chan padded)).p.n)
	gsl := out[18].([]*val)
	r = append(r, gsl[len(gsl)-1].n)
	r = append(r, out[19].(*val).n)
	fmt.Println(r)
}
