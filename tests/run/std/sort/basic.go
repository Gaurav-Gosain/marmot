package main

import "sort"

type P struct {
	name string
	age  int
}

type byAge []P

func (b byAge) Len() int           { return len(b) }
func (b byAge) Less(i, j int) bool { return b[i].age < b[j].age }
func (b byAge) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

func main() {
	xs := []int{5, 2, 8, 1, 9, 3}
	sort.Ints(xs)
	println(xs[0], xs[1], xs[5], sort.IntsAreSorted(xs))
	ss := []string{"pear", "apple", "fig"}
	sort.Strings(ss)
	println(ss[0], ss[1], ss[2])
	fs := []float64{2.5, -1, 0}
	sort.Float64s(fs)
	println(fs[0], fs[2])
	ps := []P{{"a", 30}, {"b", 20}, {"c", 25}}
	sort.Sort(byAge(ps))
	println(ps[0].name, ps[1].name, ps[2].name)
	sort.Slice(ps, func(i, j int) bool { return ps[i].name > ps[j].name })
	println(ps[0].name)
	sort.Sort(sort.Reverse(sort.IntSlice(xs)))
	println(xs[0], xs[5])
	sort.Ints(xs)
	println(sort.SearchInts(xs, 8), sort.SearchInts(xs, 4), sort.Search(len(xs), func(i int) bool { return xs[i] >= 100 }))
	big := make([]int, 1000)
	for i := range big {
		big[i] = (i * 7919) % 1009
	}
	sort.Ints(big)
	println(sort.IntsAreSorted(big), big[0], big[999])
	st := []P{{"x", 1}, {"y", 1}, {"z", 0}, {"w", 1}}
	sort.SliceStable(st, func(i, j int) bool { return st[i].age < st[j].age })
	println(st[0].name, st[1].name, st[2].name, st[3].name)
}
