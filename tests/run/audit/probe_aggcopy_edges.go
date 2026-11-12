// Edge cases for backend optimizations; the expected output is from gc.
package main

import "fmt"

type Big struct {
	a [40]int
	s string
}

var g [3]Big

//go:noinline
func mk(k int) Big {
	var b Big
	for i := range b.a {
		b.a[i] = i * k
	}
	b.s = fmt.Sprint("b", k)
	return b
}

//go:noinline
func swap(x, y *Big) { *x, *y = *y, *x }

//go:noinline
func rotate(a *[3]Big) { a[0], a[1], a[2] = a[1], a[2], a[0] }

//go:noinline
func overlap(s []int) {
	// Copy a 64-int window one element to the right through an array value.
	*(*[64]int)(s[1:]) = *(*[64]int)(s[:64])
}

//go:noinline
func readThenWrite(p *Big) Big {
	v := *p
	p.a[0] = -1
	p.s = "changed"
	return v
}

//go:noinline
func copyThenCall(p *Big, f func()) Big {
	v := *p
	f()
	return v
}

//go:noinline
func fieldOfLoad(p *Big) (int, string) {
	v := *p
	p.a[7] = 999
	return v.a[7], v.s
}

func show(b Big) string { return fmt.Sprint(b.a[0], b.a[1], b.a[39], b.s) }

func main() {
	x, y := mk(1), mk(2)
	swap(&x, &y)
	fmt.Println(show(x), show(y))
	swap(&x, &x)
	fmt.Println(show(x))
	g[0], g[1], g[2] = mk(3), mk(4), mk(5)
	rotate(&g)
	fmt.Println(show(g[0]), show(g[1]), show(g[2]))
	s := make([]int, 65)
	for i := range s {
		s[i] = i
	}
	overlap(s)
	fmt.Println(s[0], s[1], s[2], s[63], s[64])
	z := mk(6)
	v := readThenWrite(&z)
	fmt.Println(show(v), show(z))
	w := mk(7)
	c := copyThenCall(&w, func() { w.a[1] = 12345; w.s = "f" })
	fmt.Println(show(c), show(w))
	u := mk(8)
	fmt.Println(fieldOfLoad(&u))
	arr := [2]Big{mk(9), mk(10)}
	arr[0], arr[1] = arr[1], arr[0]
	fmt.Println(show(arr[0]), show(arr[1]))
	arr[1] = arr[0]
	arr[0].a[0] = 77
	fmt.Println(show(arr[0]), show(arr[1]))
}
