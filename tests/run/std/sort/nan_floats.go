package main

import (
	"math"
	"sort"
)

func main() {
	f := []float64{3, math.NaN(), 1, math.Inf(-1), 2, math.NaN(), math.Inf(1)}
	sort.Float64s(f)
	for _, v := range f {
		print(v, " ")
	}
	println()
	println(sort.Float64sAreSorted(f))
}
