// slices.Min and slices.Max (builtin min and max over a slice) when the
// slice holds NaNs with different payloads. The result is a NaN; which NaN
// (payload, sign) is not defined by the spec, and marmot returns a different
// one than gc (documented in docs/SUPPORT.md, known differences). This
// program checks only what is defined; print math.Float64bits of the results
// to see the payload difference.
package main

import (
	"fmt"
	"math"
	"slices"
)

//go:noinline
func min2(a, b float64) float64 { return min(a, b) }

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
	fmt.Println(math.IsNaN(slices.Min(x)), math.IsNaN(slices.Max(x)), math.IsNaN(min2(a, b)), math.IsNaN(min2(1, b)))
	y := build([]float64{3, math.Copysign(0, -1), 0, 1})
	fmt.Println(math.Signbit(slices.Min(y)), math.Signbit(slices.Max(y)))
}
