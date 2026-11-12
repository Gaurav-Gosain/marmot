// Audit probe (loops, 2026-11-12): adversarial program for the opt-in loop
// transforms; matched go with -par -type-alias and -loops-force-par in dev,
// -release and GC stress. See docs/audit-backend.md.
package main

import "fmt"

const N = 200000

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(name, "panic:", r)
		}
	}()
	f()
}

func sum(a []int) int {
	s := 0
	for _, v := range a {
		s += v
	}
	return s
}

//go:noinline
func divLoop(a, b, c []int) {
	for i := 0; i < len(a); i++ {
		a[i] = b[i] / c[i]
	}
}

//go:noinline
func modLoop(a, b []int, m int) {
	for i := range a {
		a[i] = b[i] % (m - i)
	}
}

//go:noinline
func nilLoop(a []int, ps []*int) {
	for i := range a {
		a[i] = *ps[i] + 1
	}
}

//go:noinline
func oobLoop(a, idx []int) {
	for i := range a {
		a[i] = idx[i] * 2
		_ = a[idx[i]]
	}
}

//go:noinline
func shortB(a, b []int) {
	for i := range a {
		a[i] = b[i] + 1 // b shorter than a: panics at len(b) after writing a prefix
	}
}

//go:noinline
func convLoop(a []int, f []float64) {
	for i := range a {
		a[i] = int(f[i]) // no panic in Go, implementation-defined for NaN/huge
	}
}

func main() {
	a := make([]int, N)
	b := make([]int, N)
	c := make([]int, N)
	for i := range b {
		b[i] = i + 1
		c[i] = 1
	}
	c[N/2+17] = 0
	try("div", func() { divLoop(a, b, c) })
	fmt.Println("after div", sum(a), a[N/2+16], a[N/2+18])

	for i := range a {
		a[i] = 0
	}
	try("mod", func() { modLoop(a, b, N/3) })
	fmt.Println("after mod", sum(a))

	ps := make([]*int, N)
	x := 7
	for i := range ps {
		ps[i] = &x
	}
	ps[N-5] = nil
	for i := range a {
		a[i] = 0
	}
	try("nil", func() { nilLoop(a, ps) })
	fmt.Println("after nil", sum(a), a[N-6], a[N-4])

	idx := make([]int, N)
	for i := range idx {
		idx[i] = i
	}
	idx[1000] = N + 5
	try("oob", func() { oobLoop(a, idx) })
	fmt.Println("after oob", sum(a))

	for i := range a {
		a[i] = 0
	}
	try("short", func() { shortB(a, b[:N-100]) })
	fmt.Println("after short", sum(a), a[N-101], a[N-100])
}
