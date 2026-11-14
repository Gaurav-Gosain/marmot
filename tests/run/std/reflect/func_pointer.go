package main

// reflect.Value.Pointer of a func is its code pointer, as in gc: what
// runtime.FuncForPC takes. Closures of one literal share it; a nil func is 0.

import (
	"fmt"
	"reflect"
	"runtime"
)

func plain() int { return 1 }

func adder(n int) func() int { return func() int { return n } }

type T struct{ n int }

func (t *T) Get() int { return t.n }

func id[X any](x X) X { return x }

func check(label string, f any) {
	p := reflect.ValueOf(f).Pointer()
	fn := runtime.FuncForPC(p)
	switch {
	case p == 0:
		fmt.Println(label, "pointer 0")
	case fn == nil:
		fmt.Println(label, "FuncForPC: nil")
	case fn.Entry() != p:
		fmt.Println(label, "Entry differs from Pointer")
	default:
		fmt.Println(label, "ok")
	}
}

func main() {
	t := &T{3}
	check("plain", plain)
	check("closure", adder(1))
	check("method value", t.Get)
	check("method expression", (*T).Get)
	check("generic", id[int])
	check("reflect method", reflect.ValueOf(t).MethodByName("Get").Interface())
	check("literal", func() {})

	a, b := adder(1), adder(2)
	fmt.Println("one literal, one code pointer:", reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer())
	fmt.Println("two functions, two code pointers:", reflect.ValueOf(a).Pointer() != reflect.ValueOf(plain).Pointer())
	var nilf func()
	fmt.Println("nil func:", reflect.ValueOf(nilf).Pointer())
}
