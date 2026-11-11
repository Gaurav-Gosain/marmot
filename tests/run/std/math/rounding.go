package main

import "math"

func main() {
	for _, v := range []float64{-2.5, -1.5, -0.5, -0.2, 0, 0.2, 0.5, 1.5, 2.5, 1e15 + 0.5, math.Inf(1), math.NaN()} {
		println(v, math.Floor(v), math.Ceil(v), math.Trunc(v), math.Round(v), math.RoundToEven(v))
	}
	i, f := math.Modf(-3.75)
	println(i, f)
}
