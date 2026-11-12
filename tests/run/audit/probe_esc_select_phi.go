// Audit probe (loops, 2026-11-12): held on HEAD 86cadb6 in dev, -release and
// GC stress. Kept as a guard; see docs/audit-backend.md.
package main

import "fmt"

//go:noinline
func pick(c bool) *int {
	x := new(int)
	y := new(int)
	*x, *y = 1, 2
	p := x
	if c {
		p = y
	}
	return p
}

//go:noinline
func viaIface(n int) any {
	x := new([4]int)
	x[0] = n
	var e any = x
	return e
}

//go:noinline
func viaAssert(e any) *[4]int { return e.(*[4]int) }

//go:noinline
func viaSlice(n int) []int {
	s := make([]int, 8)
	s[3] = n
	return s[2:5]
}

//go:noinline
func viaClosure(n int) func() int {
	x := new(int)
	*x = n
	return func() int { return *x }
}

//go:noinline
func viaStruct(n int) struct{ p *int } {
	x := new(int)
	*x = n
	return struct{ p *int }{x}
}

//go:noinline
func viaChan(n int) *int {
	c := make(chan *int, 1)
	x := new(int)
	*x = n
	c <- x
	return <-c
}

//go:noinline
func viaMap(n int) *int {
	m := map[int]*int{}
	x := new(int)
	*x = n
	m[0] = x
	return m[0]
}

//go:noinline
func viaPanic(n int) (r *int) {
	defer func() { r = recover().(*int) }()
	x := new(int)
	*x = n
	panic(x)
}

//go:noinline
func clobber() int {
	var buf [64]int
	for i := range buf {
		buf[i] = -1
	}
	s := 0
	for _, v := range buf {
		s += v
	}
	return s
}

func main() {
	a := pick(true)
	b := viaAssert(viaIface(5))
	c := viaSlice(7)
	d := viaClosure(9)
	e := viaStruct(11)
	f := viaChan(13)
	g := viaMap(15)
	h := viaPanic(17)
	clobber()
	fmt.Println(*a, b[0], c, d(), *e.p, *f, *g, *h)
}
