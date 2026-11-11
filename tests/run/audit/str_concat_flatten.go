// Audit (loops, 2026-11-12): go-strings marks string(b) no-copy when its
// only use is an inner concat with nothing writing memory in between (S3),
// then flattens that single-use inner concat into the outer one (S1). The
// bytes are then read at the outer concat, after b was modified. -release:
// "zbcx!" instead of "abcx!". -no-strings fixes it.
package main

import "fmt"

//go:noinline
func f(b []byte, suf string) string {
	s1 := string(b) + suf
	b[0] = 'z'
	return s1 + "!"
}

//go:noinline
func g(b []byte) string {
	s1 := "<" + string(b)
	b[1] = 'Q'
	s2 := s1 + "|"
	b[2] = 'R'
	return s2 + string(b)
}

func main() {
	b := []byte("abc")
	fmt.Println(f(b, "x"))
	b = []byte("abc")
	fmt.Println(g(b))
	// Same shape inline in main.
	c := []byte("hello")
	s := string(c) + "-"
	c[0] = 'J'
	fmt.Println(s + "world")
}
