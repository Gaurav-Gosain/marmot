// Audit probe (loops, 2026-11-12): adversarial program for the opt-in loop
// transforms; matched go with -par -type-alias and -loops-force-par in dev,
// -release and GC stress. See docs/audit-backend.md.
package main

import (
	"fmt"
	"unsafe"
)

const N = 300000

func sum(a []int) int {
	s := 0
	for _, v := range a {
		s += v
	}
	return s
}

//go:noinline
func copyAdd(dst, src []int) {
	for i := range dst {
		dst[i] = src[i] + 1
	}
}

//go:noinline
func strided(a []int, k, n int) {
	for i := 0; i < n; i++ {
		a[i*k] = a[i*k] + i
	}
}

//go:noinline
func stridedWrite(a []int, k, n int) {
	for i := 0; i < n; i++ {
		a[i*k] = i
	}
}

//go:noinline
func negStride(a []int, n int) {
	for i := 0; i < n; i++ {
		a[n-1-i] = a[n-1-i]*2 + i
	}
}

//go:noinline
func rows(m [][]int) {
	for i := range m {
		r := m[i]
		for j := range r {
			r[j] += i
		}
	}
}

//go:noinline
func punned(f []float64, u []uint64) {
	for i := range f {
		u[i] = uint64(i) << 52
		f[i] = f[i] + 1
	}
}

//go:noinline
func readAfterWrite(a []int, b []int) int {
	t := 0
	for i := range a {
		a[i] = i
		t += b[i]
	}
	return t
}

func main() {
	base := make([]int, N+1)
	for i := range base {
		base[i] = i
	}
	// Overlap shifted by one in both directions.
	copyAdd(base[1:], base[:N])
	fmt.Println("shift+1", sum(base), base[N])
	for i := range base {
		base[i] = i
	}
	copyAdd(base[:N], base[1:])
	fmt.Println("shift-1", sum(base), base[0])

	// Same slice in and out.
	s := make([]int, N)
	copyAdd(s, s)
	fmt.Println("same", sum(s))

	// Stride 0: every iteration updates a[0].
	a := make([]int, N)
	strided(a, 0, 1000)
	stridedWrite(a, 0, 1000)
	fmt.Println("stride0", a[0], sum(a))
	strided(a, 3, N/3)
	fmt.Println("stride3", sum(a))
	negStride(a, N)
	fmt.Println("neg", sum(a))

	// Rows that share their backing array.
	shared := make([]int, 1000)
	m := make([][]int, 400)
	for i := range m {
		if i%2 == 0 {
			m[i] = shared
		} else {
			m[i] = shared[500:]
		}
	}
	rows(m)
	fmt.Println("rows", sum(shared), shared[0], shared[999])

	// Same memory seen as float64 and uint64 (type punning).
	f := make([]float64, N)
	u := unsafe.Slice((*uint64)(unsafe.Pointer(&f[0])), N)
	punned(f, u)
	t := 0.0
	for _, v := range f {
		t += v
	}
	fmt.Println("pun", t, f[1], f[N-1])

	// Read of a slice that is the written one.
	r := make([]int, N)
	fmt.Println("raw", readAfterWrite(r, r), readAfterWrite(r, base))
}
