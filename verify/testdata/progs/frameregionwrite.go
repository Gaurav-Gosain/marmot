package main

import (
	"fmt"
	"runtime"
)

type N struct {
	next *N
	v    int
}

type list struct{ head *N }

//go:noinline
func link(o *N, v int) { // o does not escape: only its memory is written
	o.next = &N{v: v} // a heap object whose only reference is in o
}

//go:noinline
func push(l *list, v int) { // allocates in the frame region of the caller
	n := &N{v: -v}
	link(n, v)
	l.head = n
}

//go:noinline
func work() int {
	var l list
	push(&l, 7)
	for i := 0; i < 5; i++ { // the frame region is not scanned: link's object may be freed
		runtime.GC()
		j := make([]*N, 512)
		for k := range j {
			j[k] = &N{v: -1}
		}
	}
	return l.head.next.v
}

func main() { fmt.Println(work()) }
