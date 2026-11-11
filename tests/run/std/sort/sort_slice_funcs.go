package main

import "sort"

type rec struct {
	name string
	n    int
}

func main() {
	rs := []rec{{"a", 3}, {"b", 1}, {"c", 3}, {"d", 2}, {"e", 1}}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].n < rs[j].n })
	for _, r := range rs {
		print(r.name, r.n, " ")
	}
	println()
	sort.Slice(rs, func(i, j int) bool { return rs[i].name > rs[j].name })
	for _, r := range rs {
		print(r.name, " ")
	}
	println()
	println(sort.SliceIsSorted(rs, func(i, j int) bool { return rs[i].name > rs[j].name }))
}
