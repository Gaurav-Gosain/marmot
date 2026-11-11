package main

import "slices"

func main() {
	a := []string{"x", "y", "z"}
	for i, v := range slices.All(a) {
		print(i, v, " ")
	}
	println()
	for v := range slices.Values(a) {
		print(v, " ")
	}
	println()
	for i, v := range slices.Backward(a) {
		print(i, v, " ")
	}
	println()
	c := slices.Collect(slices.Values(a))
	println(len(c), c[2])
	m := slices.AppendSeq([]string{"first"}, slices.Values(a))
	println(len(m))
	println(slices.Sorted(slices.Values([]int{3, 1, 2}))[0])
	for ch := range slices.Chunk([]int{1, 2, 3, 4, 5}, 2) {
		print(len(ch), " ")
	}
	println()
}
