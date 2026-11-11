package main

import "sort"

func main() {
	n := 20000
	a := make([]int, n)
	x := uint32(12345)
	for i := range a {
		x = x*1664525 + 1013904223
		a[i] = int(x >> 8)
	}
	sort.Ints(a)
	println(sort.IntsAreSorted(a), a[0] <= a[1], a[n-1] >= a[n-2])
	for i := range a {
		a[i] = i % 10
	}
	sort.Ints(a)
	println(a[0], a[n/2], a[n-1])
	for i := range a {
		a[i] = n - i
	}
	sort.Ints(a)
	println(a[0], a[n-1])
	for i := range a {
		a[i] = 7
	}
	sort.Ints(a)
	println(a[0], sort.IntsAreSorted(a))
}
