package main

import "maps"

func main() {
	m := map[string]map[string]int{"a": {"x": 1}}
	c := maps.Clone(m)
	c["a"]["x"] = 100
	println(m["a"]["x"])
	c["b"] = map[string]int{}
	println(len(m), len(c))
	eq := maps.EqualFunc(m, c, func(a, b map[string]int) bool { return maps.Equal(a, b) })
	println(eq)
}
