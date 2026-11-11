package main

import "maps"

func main() {
	m := map[int]int{1: 1, 2: 2}
	clear(m)
	println(len(m))
	m[3] = 3
	println(len(m), maps.Equal(m, map[int]int{3: 3}))
	e := map[int]int{}
	println(maps.Equal(m, e), maps.Equal(e, map[int]int(nil)))
}
