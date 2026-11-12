// At -Os and -Oz, identical defer thunks (defer handler(1) in a, defer
// handler(2) in b) must not be merged by MergeFunctions: the extra frame
// would make recover in handler return nil. Build with -O s or -O z.
package main

import "fmt"

func handler(k int) {
	if r := recover(); r != nil {
		fmt.Println("handler", k, "recovered", r)
	}
}

// Two functions with identical defer wrappers for handler(int).
//
//go:noinline
func a() { defer handler(1); panic("a") }

//go:noinline
func b() { defer handler(2); panic("b") }

func main() {
	a()
	b()
	fmt.Println("done")
}
