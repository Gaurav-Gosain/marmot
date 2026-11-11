package main

import "sort"

func main() {
	a := []int{1, 3, 5, 7, 9, 11}
	for _, x := range []int{0, 1, 4, 5, 11, 12} {
		i := sort.SearchInts(a, x)
		println(x, i, i < len(a) && a[i] == x)
	}
	println(sort.Search(100, func(i int) bool { return i*i >= 50 }))
	s := []string{"a", "c", "e"}
	println(sort.SearchStrings(s, "d"), sort.SearchStrings(s, "z"))
	i, found := sort.Find(len(a), func(i int) int { return 7 - a[i] })
	println(i, found)
	f := []float64{1.5, 2.5}
	println(sort.SearchFloat64s(f, 2))
	println(sort.Search(0, func(int) bool { return true }))
}
