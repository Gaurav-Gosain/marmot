// adversarial.go: loops that look parallel but are not (the clauses of
// rule P). Each must run exactly like Go: same output, same panic.
package main

type shape interface{ area() int64 }
type sq struct{ s int64 }

func (q sq) area() int64 { return q.s * q.s }

var sink []int64

// Clause 4: allocation in the body (append grows a shared slice).
//go:noinline
func allocates(n int) int {
	var out []int64
	for i := 0; i < n; i++ {
		out = append(out, int64(i)*3)
	}
	sink = out
	return len(out)
}

// Clause 1: a division that may panic; the panic must happen at the first
// zero divisor, with the elements before it written.
//go:noinline
func divides(a, d []int64) {
	for i := range a {
		a[i] = a[i] / d[i]
	}
}

func tryDivide(a, d []int64) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = r.(error).Error()
		}
	}()
	divides(a, d)
	return "ok"
}

// Clause 5: writes through pointers that alias each other.
//go:noinline
func throughPointers(p []*int64) {
	for i := range p {
		*p[i] += int64(i)
	}
}

// Clause 4: channel operations in the body.
//go:noinline
func sends(n int, c chan int) {
	for i := 0; i < n; i++ {
		c <- i
	}
	close(c)
}

// Clause 4: defer in the body.
//go:noinline
func defers(n int) (t int64) {
	for i := 0; i < n; i++ {
		defer func(v int) { t = t*3 + int64(v) }(i)
	}
	return 0
}

// Clause 4: map writes in the body.
//go:noinline
func maps(n int) int {
	m := map[int]int{}
	for i := 0; i < n; i++ {
		m[i%97] += i
	}
	return m[5] + len(m)
}

// Clause 4: interface calls (unknown callee).
//go:noinline
func ifaces(s []shape) int64 {
	t := int64(0)
	for i := range s {
		t += s[i].area()
	}
	return t
}

// Clause 2: a value carried from one iteration to the next that is not a
// reduction (a recurrence).
//go:noinline
func recurrence(a []int64) {
	x := int64(1)
	for i := range a {
		x = x*1103515245 + 12345
		a[i] = x >> 16
	}
}

// Clause 6: a value used after the loop that is not a reduction.
//go:noinline
func lastIndex(a []int64, v int64) int {
	last := -1
	for i := range a {
		if a[i] == v {
			last = i
		}
	}
	return last
}

// Clause 5: the same element written by every iteration.
//go:noinline
func sameCell(a []int64, n int) {
	for i := 0; i < n; i++ {
		a[0] += int64(i)
	}
}

// Closure state captured by reference.
//go:noinline
func closures(n int) int64 {
	acc := int64(0)
	add := func(v int64) { acc += v }
	for i := 0; i < n; i++ {
		add(int64(i))
	}
	return acc
}

func main() {
	const n = 300000
	println("alloc", allocates(n), sink[n-1])

	a := make([]int64, n)
	d := make([]int64, n)
	for i := range a {
		a[i] = int64(i * 7)
		d[i] = int64(i%13 + 1)
	}
	d[123456] = 0
	println("divide", tryDivide(a, d), a[123455], a[123456], a[123457])

	x := int64(5)
	ps := make([]*int64, n)
	for i := range ps {
		ps[i] = &x
	}
	throughPointers(ps)
	println("pointers", x)

	c := make(chan int, 16)
	go sends(n, c)
	t := 0
	for v := range c {
		t += v
	}
	println("chan", t)
	println("defer", defers(40))
	println("map", maps(n))

	ss := make([]shape, 1000)
	for i := range ss {
		ss[i] = sq{int64(i)}
	}
	println("iface", ifaces(ss))

	r := make([]int64, n)
	recurrence(r)
	println("recurrence", r[0], r[n-1])
	a[777] = -3
	a[299998] = -3
	println("last", lastIndex(a, -3))
	sameCell(a, n)
	println("same", a[0])
	println("closure", closures(n))
}
