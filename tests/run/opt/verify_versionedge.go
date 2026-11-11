// versionedge: Rule V at the edges of its precondition. Each loop has bounds checks the
// pass may version; the fast copy must only run when no check can fail, and the slow copy
// must panic at exactly the iteration Go panics. Run with -par.
package main

import "fmt"

func try(name string, f func() int64) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(name, "panic:", r)
		}
	}()
	fmt.Println(name, f())
}

//go:noinline
func offset(a []int64, k, n int) (s int64) { // a[i+k]: fails only for the last iterations
	for i := 0; i < n; i++ {
		s += a[i+k]
	}
	return
}

//go:noinline
func neg(a []int64, k, n int) (s int64) { // a[i-k]: fails in the first iteration when k > 0
	for i := 0; i < n; i++ {
		s += a[i-k]
	}
	return
}

//go:noinline
func stride(a []int64, st, n int) (s int64) { // a[i*st]: large strides near the term limit
	for i := 0; i < n; i++ {
		s += a[i*st]
	}
	return
}

//go:noinline
func huge(a []int64, base, n int) (s int64) { // base near 2^62: invariant part out of range
	for i := 0; i < n; i++ {
		s += a[base+i]
	}
	return
}

//go:noinline
func le(a []int64, n int) (s int64) { // i <= n: needs the no-wrap predicate
	for i := 0; i <= n; i++ {
		s += a[i]
	}
	return
}

//go:noinline
func rowlen(b [][]int64) (s int64) { // loaded bounds: row lengths differ
	for k := range b {
		for j := 0; j < 8; j++ {
			s += b[k][j]
		}
	}
	return
}

func main() {
	a := make([]int64, 1<<20)
	for i := range a {
		a[i] = int64(i)
	}
	try("offset ok", func() int64 { return offset(a, 5, len(a)-5) })
	try("offset fails", func() int64 { return offset(a, 5, len(a)-4) })
	try("neg ok", func() int64 { return neg(a[3:], 0, len(a)-3) })
	try("neg fails", func() int64 { return neg(a, 1, len(a)) })
	try("stride ok", func() int64 { return stride(a, 1<<10, 1<<10) })
	try("stride fails", func() int64 { return stride(a, 1<<10, 1<<10+1) })
	try("huge fails", func() int64 { return huge(a, 1<<62, 10) })
	try("le fails", func() int64 { return le(a, len(a)) })
	try("le ok", func() int64 { return le(a, len(a)-1) })
	b := make([][]int64, 1000)
	for k := range b {
		b[k] = make([]int64, 8)
		b[k][7] = int64(k)
	}
	try("rowlen ok", func() int64 { return rowlen(b) })
	b[999] = b[999][:7]
	try("rowlen fails", func() int64 { return rowlen(b) })
}
