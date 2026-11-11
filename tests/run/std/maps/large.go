package main

import "maps"

func main() {
	m := map[int]int{}
	for i := 0; i < 20000; i++ {
		m[i] = i * 2
	}
	c := maps.Clone(m)
	for i := 0; i < 20000; i += 2 {
		delete(c, i)
	}
	println(len(m), len(c), maps.Equal(m, c))
	maps.DeleteFunc(m, func(k, v int) bool { return k%2 == 1 })
	println(maps.Equal(m, c), len(m))
	maps.Copy(c, m)
	println(len(c))
}
