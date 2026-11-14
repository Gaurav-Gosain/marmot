// recover through compiler wrappers: a deferred method called through a
// pointer, an interface, a method value or a method expression, and a
// generic function, recover as gc does; a helper the deferred function calls
// gets nil (wasm checks this with the recover token, see codegen target.go).
package main

import "fmt"

type T struct{ n int }

func (t T) M() {
	if r := recover(); r != nil {
		fmt.Println("M recovered", r, t.n)
	} else {
		fmt.Println("M nil")
	}
}

func (t *T) P() {
	if r := recover(); r != nil {
		fmt.Println("P recovered", r)
	}
}

type I interface{ M() }

func G[X any](x X) {
	if r := recover(); r != nil {
		fmt.Println("G recovered", r, x)
	}
}

func helper() any { return recover() }

func run(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(name, "outer caught", r)
		}
	}()
	f()
}

func main() {
	t := &T{1}
	run("a ptr->value method", func() { defer t.M(); panic("a") })
	var i I = T{2}
	run("b interface", func() { defer i.M(); panic("b") })
	var ip I = t
	run("b2 interface ptr", func() { defer ip.M(); panic("b2") })
	run("c generic", func() { defer G(3); panic("c") })
	run("c2 generic string", func() { defer G("s"); panic("c2") })
	f := t.M
	run("d method value", func() { defer f(); panic("d") })
	g := t.P
	run("d2 ptr method value", func() { defer g(); panic("d2") })
	run("neg helper", func() {
		defer func() { fmt.Println("helper got nil:", helper() == nil) }()
		panic("neg")
	})
}
