// append(s, make([]T, n)...) (MARMOT_APPENDMAKE): zeroed tail, panics for
// bad lengths, evaluation order, aliasing and stale data past len.
package main

import (
	"fmt"
	"math"
)

type pair struct {
	p *int
	s string
}

type empty struct{}

var trace []string

//go:noinline
func src(s []int) []int {
	trace = append(trace, "src")
	return s
}

//go:noinline
func size(n int) int {
	trace = append(trace, "size")
	return n
}

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%s: panic: %v\n", name, r)
		}
	}()
	f()
	fmt.Printf("%s: ok\n", name)
}

//go:noinline
func extend[T any](s []T, n int) []T { return append(s, make([]T, n)...) }

//go:noinline
func extendU64(s []byte, n uint64) []byte { return append(s, make([]byte, n)...) }

//go:noinline
func extendI8(s []int, n int8) []int { return append(s, make([]int, n)...) }

//go:noinline
func extendU8(s []int, n uint8) []int { return append(s, make([]int, n)...) }

// growSlice is the bytes.growSlice pattern.
//
//go:noinline
func growSlice(b []byte, n int) []byte {
	c := len(b) + n
	if c < 2*cap(b) {
		c = 2 * cap(b)
	}
	b2 := append([]byte(nil), make([]byte, c)...)
	i := copy(b2, b)
	return b2[:i]
}

func main() {
	// Stale data past len is cleared, in place (no growth).
	a := make([]int, 10)
	for i := range a {
		a[i] = 7
	}
	all := a
	a = a[:2]
	a = extend(a, 5)
	fmt.Println(len(a), a, all)

	// Pointers: stale pointers past len are cleared.
	x := 5
	ps := make([]pair, 4)
	for i := range ps {
		ps[i] = pair{&x, "old"}
	}
	ps = extend(ps[:1], 3)
	for i, p := range ps {
		fmt.Println(i, p.p != nil, p.s)
	}

	// Growth: old elements copied, the tail zero.
	g := []int{1, 2, 3}
	g = extend(g, 6)
	fmt.Println(len(g), g)
	var nilS []string
	nilS = extend(nilS, 3)
	fmt.Println(len(nilS), nilS[0] == "", nilS[2] == "")
	z := extend([]empty{{}}, 4)
	fmt.Println(len(z))
	fmt.Println(len(extend([]int{1}, 0)))

	// Other length types.
	fmt.Println(len(extendU8([]int{1}, 200)), len(extendI8(nil, 3)))

	// Evaluation order: the slice operand, then the length.
	trace = nil
	o := append(src([]int{9}), make([]int, size(2))...)
	fmt.Println(o, trace)

	// Bad lengths panic as make does (or as growslice, for a total that overflows).
	try("negative int", func() { _ = extend([]int{1}, -1) })
	try("negative int8", func() { _ = extendI8([]int{1}, -3) })
	try("huge uint64", func() { _ = extendU64(nil, math.MaxUint64) })
	try("negative before slice use", func() {
		trace = nil
		_ = append(src(nil), make([]int, size(-1))...)
	})
	fmt.Println(trace)

	// bytes.growSlice: copy and zeroed capacity.
	b := []byte("hello")
	b = growSlice(b, 20)
	fmt.Println(string(b), len(b), cap(b) >= 25)
	full := b[:cap(b)]
	nz := 0
	for _, c := range full[len(b):] {
		if c != 0 {
			nz++
		}
	}
	fmt.Println("nonzero tail", nz)
}
