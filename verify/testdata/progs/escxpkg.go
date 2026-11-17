// escxpkg: cross-package escape cases for MARMOT_XPKG_ESCAPE, MARMOT_ESCAPE_SUMMARY and
// MARMOT_ESCAPE_EXT. Every callee here keeps an argument past the call (in its result, a
// global, a map, a list or another goroutine), so the caller's values must outlive the frame.
// Each case clobbers the stack and collects before it reads the kept values back.
package main

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

type point struct{ x, y int }

func (p point) String() string { return fmt.Sprint(p.x, ",", p.y) }

var (
	savedAny  []any
	savedErr  error
	savedCtx  context.Context
	savedList = list.New()
	savedMap  sync.Map
	savedVal  atomic.Value
	done      = make(chan string, 1)
)

//go:noinline
func keepVariadic(xs ...any) { savedAny = xs }

//go:noinline
func returnVariadic(xs ...any) []any { return xs }

//go:noinline
func wrapErr(n int) error {
	base := fmt.Errorf("base %d", n)
	return fmt.Errorf("wrapped %w and %v", base, point{n, n + 1})
}

//go:noinline
func joinErr(n int) error {
	a := errors.New(fmt.Sprint("a", n))
	b := fmt.Errorf("b%d", n)
	return errors.Join(a, b)
}

type key struct{ k int }

//go:noinline
func withValue(n int) context.Context {
	return context.WithValue(context.Background(), key{n}, point{n, -n})
}

//go:noinline
func pushList(n int) {
	p := point{n, n * 2}
	savedList.PushBack(p)
	savedList.PushBack(&p)
}

//go:noinline
func storeMap(n int) {
	p := point{n, 7}
	savedMap.Store(n, &p)
	savedVal.Store(p)
}

//go:noinline
func goKeep(n int) {
	p := point{n, 9}
	args := []any{p, &p}
	go func() { done <- fmt.Sprint(args...) }()
}

//go:noinline
func sprintLocal(n int) string {
	p := point{n, 3}
	return fmt.Sprint("p=", p, " n=", n)
}

//go:noinline
func clobber() int {
	var a [256]int
	for i := range a {
		a[i] = -1
	}
	s := 0
	for _, v := range a {
		s += v
	}
	return s
}

func main() {
	keepVariadic(point{1, 2}, "s", 3)
	r := returnVariadic(point{4, 5}, 6)
	savedErr = wrapErr(10)
	j := joinErr(20)
	savedCtx = withValue(30)
	pushList(40)
	storeMap(50)
	goKeep(60)
	s := sprintLocal(70)
	clobber()
	runtime.GC()
	clobber()
	fmt.Println("variadic", fmt.Sprint(savedAny...))
	fmt.Println("returned", fmt.Sprint(r...))
	fmt.Println("errorf", savedErr, errors.Unwrap(savedErr))
	fmt.Println("join", j)
	fmt.Println("ctx", savedCtx.Value(key{30}))
	for e := savedList.Front(); e != nil; e = e.Next() {
		switch v := e.Value.(type) {
		case point:
			fmt.Println("list", v)
		case *point:
			fmt.Println("list ptr", *v)
		}
	}
	v, _ := savedMap.Load(50)
	fmt.Println("map", *v.(*point), savedVal.Load())
	fmt.Println("go", <-done)
	fmt.Println("sprint", s)
}
