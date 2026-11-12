// defer recover() must return nil and let the panic reach the caller
// (gc: "main recovered: b"). It faulted in dev and hung in -release and
// under MARMOT_GCSTRESS=1, also with -no-open-defers.
package main

import "fmt"

// defer recover(): the deferred call is recover itself, not a function
// that calls it, so it returns nil and the panic goes on to the caller.
//
//go:noinline
func f() (s string) {
	defer func() { s = "deferred closure ran" }()
	defer recover()
	panic("b")
}

func main() {
	defer func() { fmt.Println("main recovered:", recover()) }()
	fmt.Println(f())
}
