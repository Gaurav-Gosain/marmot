package main

import (
	"math"
	"os"
)

// ijk matmul with the size known only at run time: with -loops-rtsign the
// row stride 8n is checked positive before the loop, and the nest runs in
// parallel (2-D GPU kernel with -gpu). With n <= 0 nothing runs.
//
//go:noinline
func mmul(a, b, c []float64, n int) {
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

func main() {
	n := 190 + len(os.Args) // 191: not a compile time constant
	a, b, c := make([]float64, n*n), make([]float64, n*n), make([]float64, n*n)
	for i := range a {
		a[i] = float64(i%13)/7 - 0.75
		b[i] = float64(i%11)/3 + 0.125
	}
	mmul(a, b, c, n)
	var h uint64
	for _, v := range c {
		h = h*31 + math.Float64bits(v)
	}
	println("mmul", n, h)
	mmul(a, b, c, 0)
	mmul(a, b, c, -3)
	println("empty ok")
}
