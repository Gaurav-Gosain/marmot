package main

import "sort"

func main() {
	a := make([]int, 1000)
	for i := range a {
		a[i] = i % 3
	}
	sort.Ints(a)
	println(a[0], a[333], a[334], a[999], sort.IntsAreSorted(a))
	s := make([]string, 500)
	for i := range s {
		s[i] = string(rune('a' + i%5))
	}
	sort.Strings(s)
	println(s[0], s[100], s[499])
	println(sort.SearchInts(a, 1), sort.SearchInts(a, 2), sort.SearchInts(a, 3))
}
