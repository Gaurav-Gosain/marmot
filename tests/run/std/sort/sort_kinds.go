package main

import "sort"

func main() {
	ints := []int{5, 2, 9, 1, 5, 6, -3, 0}
	sort.Ints(ints)
	for _, v := range ints {
		print(v, " ")
	}
	println()
	f := []float64{2.5, -1, 3.25, 0, 1e9}
	sort.Float64s(f)
	for _, v := range f {
		print(v, " ")
	}
	println()
	s := []string{"banana", "apple", "Cherry", "apple2", ""}
	sort.Strings(s)
	for _, v := range s {
		print("[", v, "]")
	}
	println()
	println(sort.IntsAreSorted(ints), sort.StringsAreSorted(s), sort.Float64sAreSorted([]float64{2, 1}))
	var empty []int
	sort.Ints(empty)
	println(len(empty))
}
