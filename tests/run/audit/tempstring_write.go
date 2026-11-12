// Audit (loops, 2026-11-12): default-on no-copy string(b) (ops.go tempString,
// gc's slicebytetostringtmp) is chosen when every use is a comparison or a
// map lookup key, but the uses may come after a write to b. gc uses the
// temporary view only when the conversion is the operand itself. marmot
// (dev and -release): false and 2 instead of true and 1.
package main

import "fmt"

//go:noinline
func cmpAfterWrite(b []byte) bool {
	s := string(b) // only compared: marmot uses a temporary view of b
	b[0] = 'x'
	return s == "abc"
}

//go:noinline
func lookupAfterWrite(m map[string]int, b []byte) int {
	s := string(b)
	b[0] = 'z'
	return m[s]
}

//go:noinline
func cmpInLoop(b []byte) int {
	s := string(b)
	n := 0
	for i := 0; i < 3; i++ {
		if s == "abc" {
			n++
		}
		b[0]++
	}
	return n
}

func main() {
	fmt.Println(cmpAfterWrite([]byte("abc")))
	fmt.Println(lookupAfterWrite(map[string]int{"abc": 1, "zbc": 2}, []byte("abc")))
	fmt.Println(cmpInLoop([]byte("abc")))
}
