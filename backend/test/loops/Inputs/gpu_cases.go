package main

import (
	"math"
	"sync"
)

const n = 1 << 20

// y's range starts k elements past the pointer the kernel gets.
//
//go:noinline
func saxpyOff(a float64, x, y []float64, k int) {
	for i := 0; i < len(x); i++ {
		y[k+i] = a*x[i] + y[k+i]
	}
}

//go:noinline
func reduce(x []int64) (s, xr, lo, hi int64) {
	lo, hi = math.MaxInt64, math.MinInt64
	for i := 0; i < len(x); i++ {
		v := x[i]
		s += v
		xr ^= v
		lo = min(lo, v)
		hi = max(hi, v)
	}
	return
}

type pair struct{ a, b float64 }

//go:noinline
func fill(p []pair) {
	for i := 0; i < len(p); i++ {
		p[i].a = float64(i)
		p[i].b = -float64(i)
	}
}

//go:noinline
func roots(x, y []float64) {
	for i := 0; i < len(x); i++ {
		y[i] = math.Sqrt(x[i]*x[i] + 1)
	}
}

// Writes the even elements while another goroutine writes the odd ones: the
// loop must not copy back the odd elements (it stays on the CPU).
//
//go:noinline
func evens(y []float64) {
	for i := 0; i < len(y)/2; i++ {
		y[2*i] = 1
	}
}

// A 2-D kernel: one GPU thread per (i, j), the k loop inside the thread in
// the same order as on the CPU, so float results are the same.
//
//go:noinline
func mmul64(a, b, c []float64, n int) {
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			s := 0.0
			for k := 0; k < n; k++ {
				s += float64(a[i*n+k] * b[k*n+j])
			}
			c[i*n+j] = s
		}
	}
}

//go:noinline
func mmul32(a, b, c []float32, n int) {
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			var s float32
			for k := 0; k < n; k++ {
				s += float32(a[i*n+k] * b[k*n+j])
			}
			c[i*n+j] = s
		}
	}
}

//go:noinline
func nilWrite(p *[n]float64) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = "recovered"
		}
	}()
	for i := 0; i < n; i++ {
		p[i] = 1
	}
	return "no panic"
}

func main() {
	x := make([]float64, n)
	y := make([]float64, n+8)
	for i := range x {
		x[i] = float64(i%1000) / 8
	}
	for i := range y {
		y[i] = float64(i % 7)
	}
	saxpyOff(1.5, x, y, 5)
	println("saxpyOff", int64(y[0]), int64(y[4]), int64(y[5]*8), int64(y[n+4]*8), int64(y[n+5]), int64(y[n+7]))

	iv := make([]int64, n)
	for i := range iv {
		iv[i] = int64(i*7919%100003) - 50000
	}
	s, xr, lo, hi := reduce(iv)
	println("reduce", s, xr, lo, hi)

	p := make([]pair, n)
	fill(p)
	println("fill", int64(p[3].a), int64(p[n-1].b))

	z := make([]float64, n)
	for i := range x {
		x[i] = float64(i%201) - 100.25
	}
	roots(x, z)
	var zs float64
	for _, v := range z {
		zs += v
	}
	println("roots", int64(zs*1000))

	w := make([]float64, 2*n)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			w[2*i+1] = 2
		}
	}()
	evens(w)
	wg.Wait()
	var ws float64
	for _, v := range w {
		ws += v
	}
	println("evens", int64(ws))

	const m = 192
	a64, b64, c64 := make([]float64, m*m), make([]float64, m*m), make([]float64, m*m)
	a32, b32, c32 := make([]float32, m*m), make([]float32, m*m), make([]float32, m*m)
	for i := range a64 {
		a64[i] = float64(i%13)/7 - 0.75
		b64[i] = float64(i%11)/3 + 0.125
		a32[i], b32[i] = float32(a64[i]), float32(b64[i])
	}
	mmul64(a64, b64, c64, m)
	mmul32(a32, b32, c32, m)
	var h64, h32 uint64
	for i := range c64 {
		h64 = h64*31 + math.Float64bits(c64[i])
		h32 = h32*31 + uint64(math.Float32bits(c32[i]))
	}
	println("mmul", h64, h32)

	var np *[n]float64
	println("nil", nilWrite(np))
}
