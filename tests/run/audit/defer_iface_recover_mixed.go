// defer r.M() through an interface, where M calls recover: recovers for
// *T, and then also for a value receiver type V at the same call site.
package main

import "fmt"

type R interface{ M() }

type T struct{ name string }

// recover is called directly by the deferred call r.M(): it must stop the panic.
func (t *T) M() {
	if r := recover(); r != nil {
		fmt.Println(t.name, "recovered:", r)
	}
}

//go:noinline
func run(r R) {
	defer r.M()
	panic("boom")
}

type V struct{}

// Value receiver: called through the pointer wrapper, recover still applies.
func (V) M() {
	if r := recover(); r != nil {
		fmt.Println("value receiver recovered:", r)
	}
}

func main() {
	run(&T{"first"})
	run(V{})
	fmt.Println("done")
}
