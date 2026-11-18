// Open-coded defers and their _defer record (MARMOT_DEFERELIDE): a deferred
// call that is pending when something panics, or when Goexit runs, must
// still run. Each function defers a call that cannot panic, then performs
// one operation that may panic.
package main

import (
	"fmt"
	"math"
	"runtime"
)

var ran bool

func mark() { ran = true }

type key struct{ v any }

//go:noinline
func appendZero(s, t []struct{}) []struct{} { defer mark(); return append(s, t...) }

//go:noinline
func divide(x, y int) int { defer mark(); return x / y }

//go:noinline
func shift(x, n int) int { defer mark(); return x << n }

//go:noinline
func index(s []int, i int) int { defer mark(); return s[i] }

//go:noinline
func deref(p *int) int { defer mark(); return *p }

//go:noinline
func store(m map[string]int) { defer mark(); m["a"] = 1 }

//go:noinline
func compare(x, y key) bool { defer mark(); return x == y }

//go:noinline
func assert(x any) int { defer mark(); return x.(int) }

//go:noinline
func toArray(s []int) [4]int { defer mark(); return [4]int(s) }

//go:noinline
func makeChan(n int) chan int { defer mark(); return make(chan int, n) }

//go:noinline
func closeTwice(c chan int) { defer mark(); close(c) }

//go:noinline
func call(f func()) { defer mark(); f() }

//go:noinline
func twoDefers(x, y int) (r int) {
	defer func() { r += 100 }()
	defer mark()
	return x / y
}

//go:noinline
func safe(x int) (r int) { defer func() { r++ }(); return x * 2 }

func try(name string, f func()) {
	ran = false
	defer func() {
		r := recover()
		fmt.Printf("%s: deferred ran %v, recovered %v\n", name, ran, r)
	}()
	f()
}

func main() {
	try("append zero-size", func() { appendZero(make([]struct{}, math.MaxInt), make([]struct{}, 1)) })
	try("divide", func() { divide(1, 0) })
	try("shift", func() { shift(1, -1) })
	try("index", func() { index([]int{1}, 5) })
	try("deref", func() { deref(nil) })
	try("store", func() { store(nil) })
	try("compare", func() { compare(key{[]int{1}}, key{[]int{1}}) })
	try("assert", func() { assert("x") })
	try("to array", func() { toArray([]int{1}) })
	try("make chan", func() { makeChan(-1) })
	c := make(chan int)
	close(c)
	try("close twice", func() { closeTwice(c) })
	try("call", func() { call(func() { panic("inner") }) })
	try("two defers", func() { twoDefers(1, 0) })
	fmt.Println("two defers ok", twoDefers(10, 2))
	fmt.Println("safe", safe(20))

	done := make(chan bool)
	go func() {
		defer func() { done <- ran }()
		ran = false
		call(runtime.Goexit)
	}()
	fmt.Println("goexit: deferred ran", <-done)
}
