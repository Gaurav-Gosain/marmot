// reduce: Rule P reductions and inductions at their edges (run with -par): narrow wrapping
// sums, min/max with extremes, sub, and polynomial inductions of every width past 2^N
// iterations. Output must match stock Go.
package main

import "fmt"

const n = 1 << 20

//go:noinline
func sums(a []int8) (s8 int8, s16 int16, x uint32, mn, mx int64, sub int64) {
	mn, mx = 1<<62, -1<<62
	for i := 0; i < len(a); i++ {
		v := a[i]
		s8 += v
		s16 -= int16(v)
		x ^= uint32(v) * 2654435761
		if int64(v) < mn {
			mn = int64(v)
		}
		if int64(v) > mx {
			mx = int64(v)
		}
		sub -= int64(v)
	}
	return
}

//go:noinline
func poly8(b []int8) {
	var a, d int8
	for i := 0; i < len(b); i++ {
		d += 3
		a += d
		b[i] = a
	}
}

//go:noinline
func poly32(b []int32) {
	var a, d int32
	for i := 0; i < len(b); i++ {
		d += 7
		a += d
		b[i] = a
	}
}

//go:noinline
func poly64(b []int64) {
	var a, d int64
	for i := 0; i < len(b); i++ {
		d += 1 << 40
		a += d
		b[i] = a
	}
}

func main() {
	a := make([]int8, n)
	for i := range a {
		a[i] = int8(i*37 + i>>9)
	}
	fmt.Println(sums(a))
	b8 := make([]int8, n)
	poly8(b8)
	b32 := make([]int32, n)
	poly32(b32)
	b64 := make([]int64, n)
	poly64(b64)
	var h uint64
	for i := 0; i < n; i++ {
		h = h*1000003 ^ uint64(uint8(b8[i])) ^ uint64(uint32(b32[i]))<<8 ^ uint64(b64[i])
	}
	fmt.Println(h, b8[256], b8[n-1], b32[n-1], b64[n-1])
}
