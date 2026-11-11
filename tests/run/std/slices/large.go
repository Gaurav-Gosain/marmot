package main

import "slices"

func main() {
	n := 50000
	a := make([]int, n)
	x := uint32(7)
	for i := range a {
		x = x*1103515245 + 12345
		a[i] = int(x >> 4 % 1000)
	}
	slices.Sort(a)
	println(slices.IsSorted(a))
	a = slices.Compact(a)
	println(len(a) <= 1000, a[0])
	i, ok := slices.BinarySearch(a, a[len(a)/2])
	println(i == len(a)/2, ok)
	b := slices.Clone(a)
	slices.Reverse(b)
	println(b[0] == a[len(a)-1])
}
