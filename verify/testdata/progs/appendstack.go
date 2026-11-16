// appendstack: adversarial cases for the stack buffer of append from nil
// (MARMOT_APPENDSTACK=1). Each case must print what Go prints. No case prints cap:
// capacities of small appends differ between compilers and are not part of the contract.
package main

import (
	"fmt"
	"runtime"
)

var sink [][]int
var keep *[]int

// aliasLoop: an older slice is still live when the site runs again from nil.
//
//go:noinline
func aliasLoop() {
	var s, prev []int
	for i := 0; i < 3; i++ {
		prev = s
		s = nil
		s = append(s, i)
	}
	fmt.Println("aliasLoop", prev, s)
}

// gotoLoop: the same with a loop made of goto.
//
//go:noinline
func gotoLoop() {
	var s, prev []int
	i := 0
again:
	prev = s
	s = nil
	s = append(s, i*10)
	i++
	if i < 3 {
		goto again
	}
	fmt.Println("gotoLoop", prev, s)
}

// otherSite: two appends at other sites share the spare capacity, as in Go.
//
//go:noinline
func otherSite() {
	var s []byte
	s = append(s, 'a')
	t := append(s, 'b')
	u := append(s, 'c')
	fmt.Println("otherSite", string(t), string(u), string(s))
}

// elemPtr: a pointer to an element survives a growth that moves the slice.
//
//go:noinline
func elemPtr() {
	var s []int
	s = append(s, 1)
	p := &s[0]
	for i := 0; i < 20; i++ {
		s = append(s, i)
	}
	*p = 99
	fmt.Println("elemPtr", s[0], *p, len(s))
}

// deferRead: a deferred closure reads the slice after the body.
//
//go:noinline
func deferRead() (out int) {
	var s []int
	s = append(s, 7)
	defer func() { out = s[0] * 2 }()
	s[0] = 8
	return 0
}

// rangeFunc: a range-over-func body appends and the iterator keeps calling it.
//
//go:noinline
func rangeFunc() {
	seq := func(yield func(int) bool) {
		for i := 0; i < 4; i++ {
			if !yield(i) {
				return
			}
		}
	}
	var s []int
	for v := range seq {
		s = append(s, v*v)
	}
	fmt.Println("rangeFunc", s)
}

// recur: every frame builds its own slice and the callers' slices stay intact.
//
//go:noinline
func recur(n int) []int {
	var s []int
	s = append(s, n)
	if n > 0 {
		r := recur(n - 1)
		s = append(s, r...)
	}
	return s
}

// escapeReturn and escapeGlobal: results that leave the frame.
//
//go:noinline
func escapeReturn() []int {
	var s []int
	s = append(s, 4, 5)
	return s[:1]
}

//go:noinline
func escapeGlobal() {
	var s []int
	s = append(s, 6)
	sink = append(sink, s)
	var t []int
	t = append(t, 7)
	keep = &t
}

type node struct{ v int }

// pointers: pointer elements in the buffer across collections.
//
//go:noinline
func pointers() int {
	var s []*node
	s = append(s, &node{1}, &node{2})
	runtime.GC()
	sum := 0
	for _, p := range s {
		sum += p.v
	}
	return sum
}

// structField: the slice is stored in a local struct whose address escapes.
//
//go:noinline
func structField() {
	type box struct{ xs []int }
	var b box
	b.xs = append(b.xs, 3)
	keepBox(&b)
	b.xs[0] = 4
}

var boxed interface{}

//go:noinline
func keepBox(p interface{}) { boxed = p }

// loopGrow: one site in a loop grows past the 32-byte buffer onto the heap.
//
//go:noinline
func loopGrow(n int) int {
	var s []int
	for i := 0; i < n; i++ {
		s = append(s, i*3)
	}
	sum := 0
	for i, v := range s {
		sum += v * (i + 1)
	}
	return sum
}

// nestedFresh: a fresh slice per outer iteration, each read after the next is built.
//
//go:noinline
func nestedFresh() int {
	total := 0
	var last int
	for j := 0; j < 5; j++ {
		var s []int
		for i := 0; i <= j; i++ {
			s = append(s, i+j)
		}
		total += last * 100
		last = s[len(s)-1]
	}
	return total + last
}

// loopPointers: pointer elements built in a loop with collections in between.
//
//go:noinline
func loopPointers(n int) int {
	var s []*node
	for i := 0; i < n; i++ {
		s = append(s, &node{i})
		if i%2 == 0 {
			runtime.GC()
		}
	}
	sum := 0
	for _, p := range s {
		sum += p.v
	}
	return sum
}

func main() {
	fmt.Println("loopGrow", loopGrow(2), loopGrow(4), loopGrow(9))
	fmt.Println("nestedFresh", nestedFresh())
	fmt.Println("loopPointers", loopPointers(3), loopPointers(10))
	aliasLoop()
	gotoLoop()
	otherSite()
	elemPtr()
	fmt.Println("deferRead", deferRead())
	rangeFunc()
	fmt.Println("recur", recur(4))
	r := escapeReturn()
	escapeGlobal()
	clobber()
	fmt.Println("escape", r, sink, *keep)
	fmt.Println("pointers", pointers())
	structField()
	clobber()
	fmt.Println("structField", boxed)
}

// clobber reuses the stack where the earlier frames were.
//
//go:noinline
func clobber() {
	var a [64]int
	for i := range a {
		a[i] = -1
	}
	runtime.KeepAlive(&a)
}
