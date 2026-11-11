package main

import (
	"fmt"
	"slices"
	"sort"
)

type P struct {
	Name string
	Age  int
}

func main() {
	var ps []P
	x := 7
	for i := 0; i < 40; i++ {
		x = x*1103515245 + 12345
		ps = append(ps, P{fmt.Sprint("p", i), (x >> 8) % 5})
	}
	a := slices.Clone(ps)
	sort.SliceStable(a, func(i, j int) bool { return a[i].Age < a[j].Age })
	b := slices.Clone(ps)
	slices.SortStableFunc(b, func(p, q P) int { return p.Age - q.Age })
	println(slices.Equal(a, b))
	for i := range a {
		if a[i] != b[i] {
			println(i, a[i].Name, b[i].Name, a[i].Age, b[i].Age)
			break
		}
	}
	c := slices.Clone(ps)
	slices.SortStableFunc(c, func(p, q P) int { return p.Age - q.Age })
	println(slices.Equal(b, c))
}
