package main

import (
	"cmp"
	"slices"
)

type P struct {
	n string
	a int
}

func main() {
	xs := []int{3, 1, 4, 1, 5, 9, 2, 6}
	slices.Sort(xs)
	println(xs[0], xs[7], slices.IsSorted(xs))
	i, found := slices.BinarySearch(xs, 5)
	println(i, found)
	println(slices.Contains(xs, 9), slices.Index(xs, 4), slices.Index(xs, 100))
	println(slices.Max(xs), slices.Min(xs))
	ys := slices.Clone(xs)
	ys[0] = 100
	println(xs[0], ys[0], slices.Equal(xs, ys))
	slices.Reverse(ys)
	println(ys[0], ys[7])
	xs = slices.Compact(xs)
	println(len(xs))
	xs = slices.Insert(xs, 1, 50, 60)
	println(xs[1], xs[2], len(xs))
	xs = slices.Delete(xs, 1, 3)
	println(len(xs), xs[1])
	ps := []P{{"b", 2}, {"a", 2}, {"c", 1}}
	slices.SortFunc(ps, func(x, y P) int {
		if c := cmp.Compare(x.a, y.a); c != 0 {
			return c
		}
		return cmp.Compare(x.n, y.n)
	})
	println(ps[0].n, ps[1].n, ps[2].n)
	println(slices.IndexFunc(ps, func(p P) bool { return p.n == "b" }))
}
