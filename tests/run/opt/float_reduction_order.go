package main

import "math"

// Floating point addition is not associative. Results must match sequential evaluation.
func sum(a []float64) float64 {
	s := 0.0
	for _, v := range a {
		s += v
	}
	return s
}

func sum32(a []float32) float32 {
	var s float32
	for _, v := range a {
		s += v
	}
	return s
}

func dot(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		s += float64(a[i] * b[i]) // explicit conversion forbids fused multiply-add
	}
	return s
}

func main() {
	a := make([]float64, 1000)
	for i := range a {
		a[i] = 1 / float64(i+1)
	}
	println(sum(a))
	for i := range a {
		a[i] = math.Pow(-1, float64(i)) * 1e16 / float64(i+1)
	}
	println(sum(a))
	b := []float64{1e16, 1, -1e16, 1, 1, 1, 1e16, -1e16, 3}
	println(sum(b))
	c := make([]float32, 5000)
	for i := range c {
		c[i] = 0.1
	}
	println(sum32(c))
	x := make([]float64, 777)
	y := make([]float64, 777)
	for i := range x {
		x[i] = float64(i)*0.001 + 0.0003
		y[i] = 1e8 / float64(i+3)
	}
	println(dot(x, y))
	nan := math.NaN()
	println(sum([]float64{1, nan, 2}), sum([]float64{math.Inf(1), math.Inf(-1)}), sum([]float64{-0.0, -0.0}))
	m := math.Inf(-1)
	for _, v := range a {
		m = math.Max(m, v)
	}
	println(m)
}
