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

	var np *[n]float64
	println("nil", nilWrite(np))
}
