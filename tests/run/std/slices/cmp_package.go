package main

import (
	"cmp"
	"slices"
)

func main() {
	println(cmp.Compare(1, 2), cmp.Compare("b", "a"), cmp.Compare(2.5, 2.5), cmp.Less(1, 2))
	nan := 0.0
	nan = nan / nan
	println(cmp.Compare(nan, 1), cmp.Compare(1, nan), cmp.Compare(nan, nan), cmp.Less(nan, 1))
	println(cmp.Or(0, 0, 3, 4), cmp.Or("", "x"), cmp.Or[int]())
	println(min(1, 2), slices.Min([]float64{3, 1}))
}
