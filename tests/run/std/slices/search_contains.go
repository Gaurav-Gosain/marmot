package main

import "slices"

func main() {
	a := []int{3, 1, 4, 1, 5, 9, 2, 6}
	println(slices.Contains(a, 9), slices.Contains(a, 7), slices.Index(a, 1), slices.Index(a, 8))
	println(slices.IndexFunc(a, func(x int) bool { return x > 4 }), slices.ContainsFunc(a, func(x int) bool { return x < 0 }))
	println(slices.Max(a), slices.Min(a))
	println(slices.Equal(a, slices.Clone(a)), slices.Equal(a, a[1:]), slices.Compare([]int{1, 2}, []int{1, 3}), slices.Compare([]int{1}, []int{1}))
	defer func() { println(recover() != nil) }()
	_ = slices.Max([]int{})
}
