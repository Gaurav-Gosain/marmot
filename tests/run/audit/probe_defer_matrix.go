// Audit probe (loops, 2026-11-12): held on HEAD 86cadb6 in dev, -release and
// GC stress. Kept as a guard; see docs/audit-backend.md.
package main

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
)

func helper() any { return recover() } // not called directly by a deferred call: nil

//go:noinline
func indirectRecover() (r any) {
	defer func() { r = helper() }()
	defer func() {
		if x := recover(); x != nil {
			r = fmt.Sprint("direct:", x)
		}
	}()
	panic("p1")
}

//go:noinline
func nestedDeferRecover() (s string) {
	defer func() {
		defer func() { s += fmt.Sprint(" inner:", recover()) }()
		panic("from defer")
	}()
	s = "start"
	return
}

//go:noinline
func panicInDefer() (s string) {
	defer func() { s = fmt.Sprint("outer got:", recover()) }()
	defer func() { panic("second") }()
	panic("first")
}

//go:noinline
func loopDefers() (n int) {
	for i := 0; i < 4; i++ {
		defer func(k int) { n = n*10 + k }(i)
	}
	return 0
}

//go:noinline
func namedResult() (x int, err error) {
	defer func() {
		if r := recover(); r != nil {
			x = -1
			err = errors.New(fmt.Sprint(r))
		}
	}()
	x = 5
	var a []int
	_ = a[x]
	return x, nil
}

//go:noinline
func deferNoRecoverThenPanic() (s string) {
	defer func() { s += "+d1" }()
	defer func() { s += "+d2" }()
	s = "body"
	return
}

//go:noinline
func outerRecovers() (s string) {
	defer func() { s = fmt.Sprint("outer:", recover()) }()
	deferNoRecoverPanics()
	return "unreached"
}

var trace []string

//go:noinline
func deferNoRecoverPanics() {
	defer func() { trace = append(trace, "inner defer ran") }()
	var m map[string]int
	m["x"] = 1
}

//go:noinline
func goexitRuns() []string {
	var wg sync.WaitGroup
	var out []string
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { out = append(out, fmt.Sprint("recover in goexit:", recover())) }()
		defer func() { out = append(out, "deferred before goexit") }()
		runtime.Goexit()
	}()
	wg.Wait()
	return out
}

//go:noinline
func recoverReturnsOnce() (a, b any) {
	defer func() {
		a = recover()
		b = recover()
	}()
	panic("once")
}

type E struct{}

func (E) Error() string { return "E" }

//go:noinline
func rePanic() (s string) {
	defer func() { s = fmt.Sprint("final:", recover()) }()
	defer func() {
		r := recover()
		panic(fmt.Sprint("re:", r))
	}()
	panic(E{})
}

//go:noinline
func deferWrappedRecover() (s string) {
	defer func() { s = fmt.Sprint("got:", recover()) }()
	defer fmt.Sprint(recover()) // recover called as argument, at defer time: nil
	panic("w")
}

func try(name string, f func() string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(name, "escaped panic:", r)
		}
	}()
	fmt.Println(name, f())
}

func main() {
	fmt.Println(indirectRecover())
	fmt.Println(nestedDeferRecover())
	fmt.Println(panicInDefer())
	fmt.Println(loopDefers())
	fmt.Println(namedResult())
	fmt.Println(deferNoRecoverThenPanic())
	fmt.Println(outerRecovers(), trace)
	fmt.Println(goexitRuns())
	fmt.Println(recoverReturnsOnce())
	fmt.Println(rePanic())
	try("wrapped", deferWrappedRecover)
}
