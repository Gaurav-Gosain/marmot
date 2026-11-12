// Edge cases for the loop optimizer (-par); the expected output is from gc.
package main

import (
	"fmt"
	"math"
)

const N = 400000

//go:noinline
func prefix(p, a []int) {
	p[0] = a[0]
	for i := 1; i < len(p); i++ {
		p[i] = p[i-1] + a[i]
	}
}

//go:noinline
func prefixF(p, a []float64) {
	p[0] = a[0]
	for i := 1; i < len(p); i++ {
		p[i] = p[i-1] + a[i]
	}
}

//go:noinline
func prefixU8(p []uint8) {
	for i := 1; i < len(p); i++ {
		p[i] += p[i-1]
	}
}

//go:noinline
func hist(h []int, x []uint8) {
	for _, v := range x {
		h[v]++
	}
}

//go:noinline
func histIdx(h []int, x []int) {
	for _, v := range x {
		h[v] += 2
	}
}

//go:noinline
func count(b []byte, c byte) int {
	n := 0
	for _, v := range b {
		if v == c {
			n++
		}
	}
	return n
}

//go:noinline
func minmax(a []int) (int, int, int) {
	mn, mx, s := math.MaxInt, math.MinInt, 0
	for _, v := range a {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
		s += v
	}
	return mn, mx, s
}

//go:noinline
func lastIdx(a []int, x int) (int, int) {
	j := -1
	i := 0
	for i = 0; i < len(a); i++ {
		if a[i] == x {
			j = i
		}
	}
	return j, i
}

//go:noinline
func fsum(a []float64) float64 {
	s := 0.0
	for _, v := range a {
		s += v
	}
	return s
}

func main() {
	a := make([]int, N)
	for i := range a {
		a[i] = (i*7919)%1000 - 500
	}
	p := make([]int, N)
	prefix(p, a)
	fmt.Println("prefix", p[N/2], p[N-1])
	// In place, overlapping input and output.
	q := append([]int(nil), a...)
	prefix(q, q)
	fmt.Println("prefix inplace", q[N/2], q[N-1], q[N-1] == p[N-1])
	// Shifted overlap: reads a[i] = p[i+1], not yet written.
	w := append([]int(nil), a...)
	prefix(w[:N-1], w[1:])
	fmt.Println("prefix shifted", w[N/2], w[N-2])

	af := make([]float64, N)
	for i := range af {
		af[i] = 1e16
		if i%2 == 1 {
			af[i] = 1
		}
	}
	pf := make([]float64, N)
	prefixF(pf, af)
	fmt.Println("prefixF", pf[N-1], fsum(af))

	u8 := make([]uint8, N)
	for i := range u8 {
		u8[i] = uint8(i * 31)
	}
	prefixU8(u8)
	fmt.Println("prefixU8", u8[N-1], u8[N/3])

	x := make([]uint8, N)
	for i := range x {
		x[i] = uint8(i * 13)
	}
	h := make([]int, 256)
	hist(h, x)
	fmt.Println("hist", h[0], h[13], h[255])

	xi := make([]int, N)
	for i := range xi {
		xi[i] = i % 300
	}
	hi := make([]int, 300)
	histIdx(hi, xi)
	fmt.Println("histIdx", hi[0], hi[299])
	// Histogram whose counters alias its input.
	hx := make([]int, 600)
	for i := range hx {
		hx[i] = i % 300
	}
	histIdx(hx[:300], hx)
	fmt.Println("hist alias", hx[0], hx[1], hx[299], hx[599])
	// Out of range bin: panics after some updates.
	xi[N-3] = 300
	func() {
		defer func() { fmt.Println("hist panic", recover(), hi[0]) }()
		histIdx(hi, xi)
	}()

	bs := make([]byte, N)
	for i := range bs {
		bs[i] = byte(i % 251)
	}
	fmt.Println("count", count(bs, 7), count(bs[1:], 0))
	fmt.Println(minmax(a))
	fmt.Println(lastIdx(a, 0))
}
