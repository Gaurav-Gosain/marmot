package main

import "slices"

func main() {
	a := []int{1, 1, 2, 2, 2, 3, 1, 1}
	a = slices.Compact(a)
	println(len(a), a[0], a[1], a[2], a[3])
	s := []string{"a", "A", "b", "B", "b"}
	s = slices.CompactFunc(s, func(x, y string) bool { return x == y })
	println(len(s))
	println(len(slices.Compact([]int(nil))), len(slices.Compact([]int{7})))
}
