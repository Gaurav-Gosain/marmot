package main

import "fmt"

type N struct {
	next *N
	v    int
}

type A struct{ head *N }

var g *N

//go:noinline
func push(a *A, v int) {
	n := &N{v: v}
	old := a.head // loaded from the parameter's memory: assumed to be a region pointer
	if old != nil {
		old.next = n // but it is the caller's heap object: heap now points into the region
	}
	a.head = n
}

//go:noinline
func work(v int) {
	var a A
	a.head = g // a heap pointer in the frame object
	push(&a, v)
}

func main() {
	g = &N{v: 1}
	work(7) // g.next = region object of work's frame; freed when work returns
	saved := g.next
	g.next = nil
	work(99) // the next frame region reuses the memory
	fmt.Println(saved.v)
}
