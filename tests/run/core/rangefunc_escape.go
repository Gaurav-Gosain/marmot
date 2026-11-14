// Range over function iterators whose loop body closure is passed to a
// closure returned by a static call (codegen escape.go returnedClosure):
// with the escape summaries on, the body closure and the variables it
// captures may live in the frame. Break, return from inside the loop, a
// panic through the iterator, an iterator that keeps yield, and loops that
// run again must all behave as with gc.
package main

import (
	"fmt"
	"iter"
	"maps"
	"slices"
)

type handler func() bool

//go:noinline
func firstTrue(hs []handler, stop int) (int, bool) {
	n := 0
	for i, h := range slices.Backward(hs) {
		n += i
		if h() {
			return n, true
		}
		if i == stop {
			break
		}
	}
	return n, false
}

var keptYield func(int) bool

// keeper keeps yield: the body closure must outlive the loop.
func keeper(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		keptYield = yield
		for i := range n {
			if !yield(i) {
				return
			}
		}
	}
}

//go:noinline
func sumKept(n int) (s int) {
	for v := range keeper(n) {
		s += v
	}
	return s
}

//go:noinline
func panics(xs []int) (r string) {
	defer func() {
		if e := recover(); e != nil {
			r = fmt.Sprint("recovered ", e)
		}
	}()
	for i, x := range slices.All(xs) {
		if x < 0 {
			panic(fmt.Sprint("negative at ", i))
		}
	}
	return "no panic"
}

//go:noinline
func nested(m map[string][]int) []string {
	var out []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		total := 0
		for _, v := range slices.Backward(m[k]) {
			total = total*10 + v
		}
		out = append(out, fmt.Sprint(k, total))
	}
	return out
}

func main() {
	var calls []int
	hs := []handler{
		func() bool { calls = append(calls, 0); return false },
		func() bool { calls = append(calls, 1); return true },
		func() bool { calls = append(calls, 2); return false },
	}
	for k := 0; k < 3; k++ {
		fmt.Println(firstTrue(hs, -1))
		fmt.Println(firstTrue(hs, 2))
	}
	fmt.Println(calls)
	fmt.Println(sumKept(5), sumKept(3))
	fmt.Println(panics([]int{1, 2, 3}), panics([]int{1, -2, 3}))
	fmt.Println(nested(map[string][]int{"b": {1, 2, 3}, "a": {4, 5}}))
}
