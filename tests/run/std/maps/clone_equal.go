package main

import "maps"

func main() {
	m := map[string]int{"a": 1, "b": 2}
	c := maps.Clone(m)
	c["c"] = 3
	println(len(m), len(c), maps.Equal(m, c))
	delete(c, "c")
	println(maps.Equal(m, c))
	var nm map[string]int
	println(maps.Clone(nm) == nil, maps.Equal(nm, map[string]int{}))
	println(maps.EqualFunc(m, c, func(a, b int) bool { return a == b }))
}
