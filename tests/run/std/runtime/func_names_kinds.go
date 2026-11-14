// gc names of generic functions and their closures, method values and
// expressions of generic types, unexported method values, nested closures,
// package initializer closures, and functions found by runtime.Caller.
package main

import (
	"fmt"
	"reflect"
	"runtime"
)

func plain() int { return 1 }

func adder(n int) func() int { return func() int { return n } }

type T struct{ n int }

func (t *T) Get() int { return t.n }
func (t T) Val() int  { return t.n }
func (t *T) hid() int { return t.n }

type G[X any] struct{ x X }

func (g *G[X]) M() X { return g.x }

func Gen[X any](x X) func() X { return func() X { return x } }

var initf = func() int { return 2 }

func init() { _ = initf }
func init() {}

func nested() func() func() int {
	return func() func() int { return func() int { return 1 } }
}

func name(f any) string {
	if fn := runtime.FuncForPC(reflect.ValueOf(f).Pointer()); fn != nil {
		return fn.Name()
	}
	return "unknown"
}

func main() {
	t := &T{3}
	fmt.Println(name(plain))
	fmt.Println(name(adder(1)))
	fmt.Println(name(t.Get))
	fmt.Println(name(T{}.Val))
	fmt.Println(name((*T).Get))
	fmt.Println(name(T.Val))
	fmt.Println(name((*T).Val))
	fmt.Println(name(t.hid))
	fmt.Println(name(func() {}))
	fmt.Println(name(func() int { return t.n }))
	fmt.Println(name(fmt.Println))
	fmt.Println(name(Gen[int](1)))
	fmt.Println(name(Gen[string]))
	g := &G[int]{}
	fmt.Println(name(g.M))
	fmt.Println(name((*G[int]).M))
	fmt.Println(name(initf))
	fmt.Println(name(nested()))
	fmt.Println(name(nested()()))
	pc, _, _, _ := runtime.Caller(0)
	fmt.Println(runtime.FuncForPC(pc).Name())
	func() {
		pc, _, _, _ := runtime.Caller(0)
		fmt.Println(runtime.FuncForPC(pc).Name())
	}()
}
