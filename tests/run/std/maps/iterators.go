package main

import (
	"maps"
	"slices"
)

func main() {
	m := map[string]int{"x": 1, "y": 2, "z": 3}
	k := slices.Sorted(maps.Keys(m))
	v := slices.Sorted(maps.Values(m))
	println(len(k), k[0], k[2], v[0], v[2])
	t := 0
	for key, val := range maps.All(m) {
		t += len(key) * val
	}
	println(t)
	n := maps.Collect(maps.All(m))
	println(len(n), maps.Equal(m, n))
	maps.Insert(n, maps.All(map[string]int{"w": 9}))
	println(len(n), n["w"])
	println(len(slices.Collect(maps.Keys(map[int]bool{}))))
}
