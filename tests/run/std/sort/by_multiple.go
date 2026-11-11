package main

import "sort"

type P struct {
	Last, First string
	Age         int
}

func main() {
	ps := []P{{"Smith", "John", 30}, {"Doe", "Jane", 25}, {"Smith", "Anna", 41}, {"Doe", "Al", 25}}
	sort.Slice(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		if a.Last != b.Last {
			return a.Last < b.Last
		}
		if a.Age != b.Age {
			return a.Age < b.Age
		}
		return a.First < b.First
	})
	for _, p := range ps {
		println(p.Last, p.First, p.Age)
	}
}
