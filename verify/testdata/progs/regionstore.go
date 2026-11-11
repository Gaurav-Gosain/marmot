package main

import "fmt"

type T struct {
	p, c *T
	v    int
}

//go:noinline
func build(h *T, val int) *T {
	n := &T{}
	m := &T{v: val}
	n.c = m // region object into region object: allowed
	n.p = h // a heap pointer stored into the region object
	r := n.c
	q := n.p // loaded back out: the pass counts q as part of the region
	q.p = r  // so this store is allowed: heap object h now points into the region
	return n
}

//go:noinline
func use(h *T, val int) bool {
	x := build(h, val) // only compared: confined, the region is freed after this block
	return x != nil
}

func main() {
	h := &T{v: 1}
	ok := use(h, 7)
	h2 := &T{v: 2}
	ok2 := use(h2, 99) // a second region reuses the freed memory
	fmt.Println(ok, ok2, h.p.v, h2.p.v)
}
