// nocopyuses: the uses go-strings S3 treats as transient (map lookup and delete keys, the
// length, string compares), each followed by a write to the bytes; with no-copy views the
// results must still be those of copies. Run with MARMOT_BACKEND_FLAGS=-strings-nocopy.
package main

import "fmt"

//go:noinline
func run(b []byte, m map[string]int) []int {
	var out []int
	v, ok := m[string(b)] // lookup key
	b[0] = 'q'
	out = append(out, v, len(m), map[bool]int{true: 1}[ok])
	delete(m, string(b)) // delete key ("qbc" is not in the map)
	b[0] = 'a'
	out = append(out, len(m))
	n := len(string(b)) // length only
	b[1] = 'z'
	out = append(out, n)
	eq := string(b) == "azc" // compare
	b[2] = 'y'
	out = append(out, map[bool]int{true: 1}[eq])
	lt := string(b) < "b" // cmpstring
	b[0] = 'c'
	out = append(out, map[bool]int{true: 1}[lt])
	for k := range m {
		out = append(out, len(k), int(k[0]))
	}
	return out
}

func main() {
	fmt.Println(run([]byte("abc"), map[string]int{"abc": 7}))
}
