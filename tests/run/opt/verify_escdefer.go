// escdefer: escape analysis and open-coded defers at the edges of their rules.
package main

import "fmt"

type node struct {
	v    int
	next *node
}

var global *node

//go:noinline
func loopAlloc(n int) int { // allocation in a loop kept across iterations: must escape
	var prev *node
	for i := 0; i < n; i++ {
		x := &node{v: i, next: prev}
		prev = x
	}
	s := 0
	for p := prev; p != nil; p = p.next {
		s += p.v
	}
	return s
}

//go:noinline
func loopLocal(n int) int { // allocation in a loop not kept: may use one slot
	s := 0
	for i := 0; i < n; i++ {
		x := &node{v: i}
		s += x.v
	}
	return s
}

//go:noinline
func storeGlobal(i int) { // stored into a global: escapes
	x := &node{v: i}
	global = x
}

//go:noinline
func viaIface(i int) interface{} { // converted to an interface and returned: escapes
	x := &node{v: i}
	return x
}

func deferOrder() (out []int) {
	defer func() { out = append(out, 1) }()
	defer func() { out = append(out, 2) }()
	defer func() { out = append(out, 3) }()
	return nil
}

func deferPanic() (out []string) {
	defer func() {
		r := recover()
		out = append(out, fmt.Sprint("recovered ", r))
	}()
	defer func() { out = append(out, "second") }()
	defer func() {
		out = append(out, "first")
		panic("in deferred call")
	}()
	panic("original")
}

func deferBranch(c bool) (out []int) {
	defer func() { out = append(out, 0) }()
	if c {
		defer func() { out = append(out, 1) }()
	} else {
		defer func() { out = append(out, 2) }()
	}
	defer func() { out = append(out, 3) }()
	return nil
}

func main() {
	fmt.Println(loopAlloc(1000), loopLocal(1000))
	storeGlobal(7)
	fmt.Println(global.v, viaIface(9).(*node).v)
	fmt.Println(deferOrder())
	fmt.Println(deferPanic())
	fmt.Println(deferBranch(true), deferBranch(false))
}
