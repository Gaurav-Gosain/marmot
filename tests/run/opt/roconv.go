// []byte(s) arguments (MARMOT_ROCONV): a conversion passed to a callee that
// only reads it may alias the string; a callee that writes, keeps or
// passes it on must get a copy. Strings here are constants (read-only
// memory: a missed write crashes) and built strings (a missed write
// changes them).
package main

import (
	"bytes"
	"fmt"
	"strings"
	"unsafe"
)

var kept []byte
var keptElem *byte

//go:noinline
func sum(b []byte) (n int) {
	for _, c := range b {
		n += int(c)
	}
	return
}

//go:noinline
func first(b []byte) byte { return b[0] }

//go:noinline
func store(b []byte) { b[0] = 'X' }

//go:noinline
func appendInto(b []byte) []byte { return append(b[:0], 'Y') }

//go:noinline
func copyInto(b []byte) { copy(b, "ZZ") }

//go:noinline
func viaWriter(b []byte) { store(b) }

//go:noinline
func keep(b []byte) { kept = b }

//go:noinline
func keepElem(b []byte) { keptElem = &b[0] }

//go:noinline
func arrayWrite(b []byte) { (*[2]byte)(b)[1] = 'W' }

//go:noinline
func unsafeWrite(b []byte) { *unsafe.SliceData(b) = 'U' }

//go:noinline
func viaFunc(b []byte, f func([]byte)) { f(b) }

//go:noinline
func deferWrite(b []byte) { defer store(b) }

//go:noinline
func readThenReturn(b []byte) int { return len(b) + int(b[len(b)-1]) }

func built(n int) string { return strings.Repeat("ab", n) }

func main() {
	const c = "hello"
	s := built(3)

	fmt.Println(sum([]byte(c)), first([]byte(c)), sum([]byte(s)), first([]byte(s)), readThenReturn([]byte(s)))
	fmt.Println(bytes.Equal([]byte(c), []byte("hello")), bytes.IndexByte([]byte(s), 'b'))

	store([]byte(c))
	store([]byte(s))
	fmt.Println(c, s)
	r := appendInto([]byte(s))
	fmt.Println(string(r), s)
	copyInto([]byte(c))
	copyInto([]byte(s))
	viaWriter([]byte(s))
	keep([]byte(c))
	kept[0] = 'K'
	keepElem([]byte(s))
	*keptElem = 'E'
	arrayWrite([]byte(c))
	arrayWrite([]byte(s))
	unsafeWrite([]byte(s))
	viaFunc([]byte(c), store)
	deferWrite([]byte(s))
	fmt.Println(c, s, string(kept))

	// string(b) then []byte(s) to a reader.
	b := []byte("view")
	v := string(b)
	b[0] = 'V'
	fmt.Println(sum([]byte(v)), v)
}
