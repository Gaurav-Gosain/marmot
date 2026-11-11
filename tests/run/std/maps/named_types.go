package main

import "maps"

type Set map[string]struct{}
type Counts map[string]int

func main() {
	s := Set{"a": {}, "b": {}}
	c := maps.Clone(s)
	c["c"] = struct{}{}
	println(len(s), len(c))
	cn := Counts{"x": 1}
	maps.Copy(cn, Counts{"y": 2})
	println(len(cn), cn["y"])
	var cc Counts = maps.Clone(cn)
	println(cc["x"])
}
