// Audit (loops, 2026-11-12): defer r.M() through an interface, where M
// calls recover. Recovers for *T, then a call with a value receiver type
// V at the same site is not recovered in dev mode (-release is right; each
// type alone is right). Not the backend open-defers pass.
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
