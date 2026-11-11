// stackconv: conversions and small makes that MARMOT_STACKCONV may put in 32 byte frame buffers;
// each result leaves the function by a different route and must survive later calls.
package main

import "fmt"

var keepS string
var keepB []byte
var keepI interface{}
var m = map[string]int{}

//go:noinline
func storeS(s string) { keepS = s }

//go:noinline
func storeB(b []byte) { keepB = b }

//go:noinline
func idS(s string) string { return s }

//go:noinline
func clobber() int {
	var a [64]byte
	for i := range a {
		a[i] = '#'
	}
	return int(a[3])
}

//go:noinline
func run(b []byte, s string) (out []string) {
	storeS(string(b)) // conversion result kept by the callee
	clobber()
	out = append(out, keepS)
	t := idS(string(b[:3])) // returned through an identity callee
	clobber()
	out = append(out, t)
	storeB([]byte(s)) // []byte(s) kept by the callee
	clobber()
	out = append(out, string(keepB))
	x := make([]byte, 4) // small make, kept through an interface
	copy(x, "make")
	keepI = x
	clobber()
	out = append(out, string(keepI.([]byte)))
	m[string(b[:2])] = 7 // map key: the map keeps a copy
	clobber()
	for k := range m {
		out = append(out, k)
	}
	c := []byte("const") // []byte of a constant, returned
	out = append(out, string(retB(c)))
	return
}

//go:noinline
func retB(c []byte) []byte { return c }

func main() {
	fmt.Println(run([]byte("hello"), "world"))
}
