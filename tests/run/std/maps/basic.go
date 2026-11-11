package main

import (
	"maps"
	"slices"
)

func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	keys := slices.Sorted(maps.Keys(m))
	println(len(keys), keys[0], keys[2])
	vals := slices.Sorted(maps.Values(m))
	println(vals[0], vals[2])
	c := maps.Clone(m)
	c["d"] = 4
	println(len(m), len(c), maps.Equal(m, c))
	maps.DeleteFunc(c, func(k string, v int) bool { return v%2 == 0 })
	println(len(c))
	maps.Copy(c, map[string]int{"z": 26})
	println(c["z"], len(c))
	n := 0
	for k, v := range maps.All(m) {
		n += len(k) + v
	}
	println(n)
}
