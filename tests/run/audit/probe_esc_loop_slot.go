// Audit probe (loops, 2026-11-12): held on HEAD 86cadb6 in dev, -release and
// GC stress. Kept as a guard; see docs/audit-backend.md.
package main

import "fmt"

type node struct {
	v    int
	next *node
}

//go:noinline
func keep(p *int) int { return *p }

// Allocations in loops share one stack slot only if no instance outlives
// its iteration.
func main() {
	// Pointer from the previous iteration kept in a local (SSA phi).
	var prev *int
	sum := 0
	for i := 0; i < 5; i++ {
		p := new(int)
		*p = i
		if prev != nil {
			sum += *prev * 10
		}
		sum += keep(p)
		prev = p
	}
	fmt.Println(sum, *prev)

	// Linked list built through a struct field.
	var head *node
	for i := 0; i < 4; i++ {
		head = &node{i, head}
	}
	for n := head; n != nil; n = n.next {
		fmt.Print(n.v, " ")
	}
	fmt.Println()

	// Slices of constant size in a loop, the previous one kept.
	var last []int
	for i := 0; i < 3; i++ {
		s := make([]int, 4)
		s[0] = i
		if last != nil {
			s[1] = last[0] + 100
		}
		last = s
	}
	fmt.Println(last)

	// Closures in a loop, called after the loop.
	var fs []func() int
	for i := 0; i < 3; i++ {
		x := new(int)
		*x = i * i
		fs = append(fs, func() int { return *x })
	}
	for _, f := range fs {
		fmt.Print(f(), " ")
	}
	fmt.Println()

	// Deferred closures created in a loop.
	func() {
		for i := 0; i < 3; i++ {
			x := new(int)
			*x = i
			defer func() { fmt.Print("d", *x, " ") }()
		}
	}()
	fmt.Println()

	// Pointer comparisons across iterations.
	var a, b *int
	for i := 0; i < 2; i++ {
		p := new(int)
		if i == 0 {
			a = p
		} else {
			b = p
		}
	}
	fmt.Println(a == b)
}
