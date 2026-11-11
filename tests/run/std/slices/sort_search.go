package main

import (
	"cmp"
	"slices"
)

func main() {
	a := []int{5, 2, 8, 1, 9, 3}
	slices.Sort(a)
	println(a[0], a[5], slices.IsSorted(a))
	i, ok := slices.BinarySearch(a, 8)
	println(i, ok)
	i, ok = slices.BinarySearch(a, 4)
	println(i, ok)
	slices.SortFunc(a, func(x, y int) int { return cmp.Compare(y, x) })
	println(a[0], a[5])
	i, ok = slices.BinarySearchFunc(a, 5, func(e, t int) int { return cmp.Compare(t, e) })
	println(i, ok)
	s := []string{"b", "A", "c"}
	slices.Sort(s)
	println(s[0], s[1], s[2])
	println(slices.IsSortedFunc(s, func(a, b string) int { return cmp.Compare(a, b) }))
}
