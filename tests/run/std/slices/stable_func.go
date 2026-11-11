package main

import "slices"

type P struct {
	n string
	a int
}

func main() {
	ps := []P{{"a", 2}, {"b", 1}, {"c", 2}, {"d", 1}}
	slices.SortStableFunc(ps, func(x, y P) int { return x.a - y.a })
	for _, p := range ps {
		print(p.n, " ")
	}
	println()
	println(slices.MaxFunc(ps, func(x, y P) int { return x.a - y.a }).n, slices.MinFunc(ps, func(x, y P) int { return x.a - y.a }).n)
}
