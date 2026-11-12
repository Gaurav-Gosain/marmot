// slices.Min and slices.Max (builtin min and max over a slice) when the
// slice holds NaNs with different payloads: the result is a NaN in both, but
// gc returns a specific one (the first NaN reached); marmot another.
package main

import (
	"fmt"
	"math"
	"slices"
)

//go:noinline
func min2(a, b float64) float64 { return min(a, b) }

//go:noinline
func max2(a, b float64) float64 { return max(a, b) }

//go:noinline
func build(v []float64) []float64 {
	x := make([]float64, 0, len(v))
	for _, f := range v {
		x = append(x, f)
	}
	return x
}

func main() {
	a := math.Float64frombits(0x7ff8000000000001)
	b := math.Float64frombits(0xfff8000000000000)
	x := build([]float64{-2, 2, a, b, 0, a, a, 2, -3, a, 2, a})
	fmt.Printf("min %#x max %#x\n", math.Float64bits(slices.Min(x)), math.Float64bits(slices.Max(x)))
	fmt.Printf("min(a,b) %#x min(b,a) %#x max(a,b) %#x max(b,a) %#x\n",
		math.Float64bits(min2(a, b)), math.Float64bits(min2(b, a)), math.Float64bits(max2(a, b)), math.Float64bits(max2(b, a)))
	fmt.Printf("min(a,1) %#x min(1,a) %#x\n", math.Float64bits(min2(a, 1)), math.Float64bits(min2(1, a)))
}
