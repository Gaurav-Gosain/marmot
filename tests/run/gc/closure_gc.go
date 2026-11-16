// Closures keep their captured variables across collections. On wasm
// (4-byte pointers) the gcdata bitmap once used 8-byte words, so the
// captured pointer at offset 4 of a closure context was never scanned and
// most closures returned a wrong value after runtime.GC.
package main

import (
	"fmt"
	"runtime"
)

var sink [][]byte

func main() {
	const n = 5000
	fs := make([]func() int, n)
	for i := range n {
		fs[i] = func() int { return i }
	}
	runtime.GC()
	for range 2000 {
		sink = append(sink, make([]byte, 256))
	}
	runtime.GC()
	bad := 0
	for i, f := range fs {
		if f() != i {
			bad++
		}
	}
	fmt.Println("closures wrong:", bad)
}
