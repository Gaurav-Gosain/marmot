// Audit probe (loops, 2026-11-12): adversarial program for the opt-in loop
// transforms; matched go with -par -type-alias and -loops-force-par in dev,
// -release and GC stress. See docs/audit-backend.md.
package main

import "fmt"

const N = 300000

//go:noinline
func ivOut(a []int, n int) (int, int, int) {
	i, k, q := 0, 5, 0
	for i = 0; i < n; i++ {
		a[i] = k
		k += 3
		q += i // polynomial
	}
	return i, k, q
}

//go:noinline
func poly16(a []int16, n int) int16 {
	var t int16
	var q int16
	for i := 0; i < n; i++ {
		q += int16(i)
		a[i] = q
		t = q
	}
	return t
}

//go:noinline
func lastStore(a []int, n int) int {
	v := 0
	for i := 0; i < n; i++ {
		v = i * i
		a[i] = v
	}
	return v
}

//go:noinline
func zeroTrip(a []int, n int) (int, int) {
	k := 42
	i := 0
	for i = 0; i < n; i++ {
		a[i] = k
		k++
	}
	return i, k
}

//go:noinline
func appendLoop(n int) []int {
	var s []int
	for i := 0; i < n; i++ {
		s = append(s, i*2)
	}
	return s
}

//go:noinline
func appendPre(s []int, n int) []int {
	for i := 0; i < n; i++ {
		s = append(s, s[i]+1)
	}
	return s
}

func main() {
	a := make([]int, N)
	fmt.Println(ivOut(a, N))
	b := make([]int16, N)
	fmt.Println(poly16(b, N), b[N-1], b[70000])
	fmt.Println(lastStore(a, N))
	fmt.Println(zeroTrip(a, 0))
	fmt.Println(zeroTrip(a, -5))
	s := appendLoop(N)
	fmt.Println(len(s), cap(s) >= N, s[N-1])
	t := make([]int, 1, 4)
	t = appendPre(t, N)
	fmt.Println(len(t), t[N])
	// Aliasing append: grow inside a shared backing array.
	base := make([]int, 10, N*2)
	x := base[:5]
	x = appendPre(x, N)
	fmt.Println(len(x), base[5], base[9], x[N])
}
