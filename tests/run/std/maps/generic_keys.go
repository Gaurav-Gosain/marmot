package main

import "maps"

type K struct {
	a int
	b string
}

func main() {
	m := map[K][]int{{1, "a"}: {1, 2}}
	c := maps.Clone(m)
	c[K{1, "a"}][0] = 99
	println(m[K{1, "a"}][0])
	m2 := map[[2]int]bool{{1, 2}: true}
	println(maps.Equal(m2, maps.Clone(m2)))
	fm := map[float64]int{1.5: 1}
	println(maps.Clone(fm)[1.5])
	im := map[interface{}]int{"a": 1, 2: 2}
	println(len(maps.Clone(im)))
}
