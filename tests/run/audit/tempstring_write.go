// A no-copy string(b) used only in comparisons or as a map key must not
// see writes to b made after the conversion (gc uses the temporary view
// only when the conversion is the operand itself).
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
