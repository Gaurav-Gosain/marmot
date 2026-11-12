// regionrec: region values (MARMOT_REGIONS, MARMOT_FRAME_REGIONS) passed to callees whose
// escape and store summaries need recursion or several levels: a mutually recursive pair
// where only the second function leaks a pointer loaded from its argument (the optimistic
// assumption for the running function must be dropped), a pointer store two calls deep into
// the argument's memory (frame regions are not scanned), and a caller storing a heap pointer
// it loaded from a global into an object it allocated. Run under MARMOT_POISON=1
// MARMOT_GCSTRESS=1; the output must equal stock Go's.
package main

import (
	"fmt"
	"runtime"
)

type N struct {
	v    int
	next *N
}

var g *N
var heap = &N{v: 500}

func gc() {
	for i := 0; i < 3; i++ {
		runtime.GC()
		junk := make([]*N, 256)
		for j := range junk {
			junk[j] = &N{v: -1}
		}
		_ = junk
	}
}

//go:noinline
func walkA(x *N, d int) int {
	if x == nil || d == 0 {
		return 0
	}
	return x.v + walkB(x.next, d-1)
}

//go:noinline
func walkB(x *N, d int) int {
	if x == nil {
		return 0
	}
	if d == 3 {
		g = x // leaks a pointer loaded out of the argument's graph
	}
	return walkA(x, d-1)
}

//go:noinline
func build(v int) *N {
	n := &N{v: v}
	n.next = &N{v: v + 1, next: &N{v: v + 2}}
	return n
}

//go:noinline
func useRec(v int) int {
	x := build(v)
	return walkA(x, 6)
}

//go:noinline
func link1(o *N, v int) { link2(o, v) }

//go:noinline
func link2(o *N, v int) { o.next = &N{v: v} }

type L struct{ head *N }

//go:noinline
func push(l *L, v int) {
	n := &N{v: -v}
	link1(n, v)
	l.head = n
}

//go:noinline
func readBack(l *L) int { return l.head.v + l.head.next.v }

//go:noinline
func callerStore(v int) int {
	n := &N{v: v}
	n.next = heap // a heap pointer stored into an object that may be region allocated
	keep := &L{head: n}
	gc()
	return keep.head.next.v + v
}

func main() {
	r := []int{useRec(10)}
	gc()
	r = append(r, g.v)
	var l L
	push(&l, 7)
	gc()
	r = append(r, readBack(&l))
	heap = &N{v: 600}
	r = append(r, callerStore(3))
	gc()
	r = append(r, heap.v)
	fmt.Println(r)
}
