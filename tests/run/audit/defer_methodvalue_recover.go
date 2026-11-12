// A deferred method value (f := t.M; defer f()) whose method calls
// recover: M is the deferred function, so recover stops the panic.
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
