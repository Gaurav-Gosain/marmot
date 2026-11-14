// A deferred call that returned normally ran its own defers; a later
// function they call does not recover the panic still in progress (gc).
package main

import "fmt"

func F() {
	fmt.Println("F recover:", recover())
}

func outer() {
	defer F()
}

func main() {
	defer func() { fmt.Println("main caught:", recover()) }()
	defer outer()
	panic("P")
}
