package main

import "fmt"

type T struct {
	p *T
	v int
}

var g *T

//go:noinline
func build(v int) *T { // region cloneable: a fresh graph of two objects
	n := &T{v: v}
	n.p = &T{v: v + 1}
	return n
}

//go:noinline
func peek(x *T) { // x itself does not escape, but the pointer loaded out of it does
	g = x.p
}

//go:noinline
func use(v int) {
	x := build(v) // confined: x only passed to a non-escaping parameter
	peek(x)
}

func main() {
	use(7)
	saved := g
	use(99) // the next region reuses the freed memory
	fmt.Println(saved.v)
}
