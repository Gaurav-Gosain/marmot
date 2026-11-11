package main

import "errors"

func main() {
	a := errors.New("same")
	b := errors.New("same")
	m := map[error]int{a: 1, b: 2}
	println(len(m), m[a], m[b])
	var e1, e2 error = a, a
	println(e1 == e2, e1 == b)
	println(errors.Is(a, b), errors.Is(a, a))
}
