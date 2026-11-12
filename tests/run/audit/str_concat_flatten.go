// go-strings must not read a no-copy string(b) after b changes: a
// single-use inner concat flattened into the outer one read b after
// b[0] was written (-release printed "zbcx!" for "abcx!").
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
