// Audit (loops, 2026-11-12): a deferred method value f := t.M; defer f()
// whose method calls recover. gc: M is the deferred function, so recover
// stops the panic. marmot (dev and -release): the panic is not recovered.
// Not the backend open-defers pass (-no-open-defers changes nothing).
package main

import "fmt"

type T struct{ name string }

// recover is called directly by the deferred method value f: it must stop the panic.
func (t *T) M() {
	if r := recover(); r != nil {
		fmt.Println(t.name, "recovered:", r)
	}
}

//go:noinline
func run(t *T) {
	f := t.M
	defer f()
	panic("method value")
}

func main() {
	run(&T{"mv"})
	fmt.Println("done")
}
