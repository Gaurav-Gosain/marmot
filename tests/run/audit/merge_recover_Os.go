// Audit (loops, 2026-11-12): at -Os and -Oz, LLVM MergeFunctions turns the
// defer thunk of b (defer handler(2)) into a call to the identical thunk
// of a. keepRecoverFramesUnmerged only guards functions that call
// runtime.gorecover, not the thunks gorecover counts as "exactly one
// compiler generated thunk", so the extra frame makes recover in handler
// return nil and the panic in b is not recovered. -O2 is right.
// Build with -O s or -O z to see it.
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
