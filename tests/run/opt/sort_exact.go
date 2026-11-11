package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

type P struct {
	Name string
	Age  int
}

func main() {
	x := uint32(99)
	next := func() int {
		x = x*1664525 + 1013904223
		return int(x >> 12)
	}
	for _, n := range []int{0, 1, 2, 3, 11, 12, 13, 50, 100, 1000} {
		a := make([]int, n)
		for i := range a {
			a[i] = next() % 50
		}
		b := slices.Clone(a)
		c := slices.Clone(a)
		sort.Ints(a)
		slices.Sort(b)
		sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
		fmt.Println(n, slices.Equal(a, b), slices.Equal(a, c), sort.IntsAreSorted(a))
	}
	// Stability and exact order for equal keys.
	var ps []P
	for i := 0; i < 40; i++ {
		ps = append(ps, P{fmt.Sprint("p", i), next() % 5})
	}
	st := slices.Clone(ps)
	sort.SliceStable(st, func(i, j int) bool { return st[i].Age < st[j].Age })
	var names []string
	for _, p := range st {
		names = append(names, p.Name)
	}
	fmt.Println(strings.Join(names, ","))
	st2 := slices.Clone(ps)
	slices.SortStableFunc(st2, func(a, b P) int { return a.Age - b.Age })
	fmt.Println(slices.Equal(st, st2))
	us := slices.Clone(ps)
	sort.Slice(us, func(i, j int) bool { return us[i].Age < us[j].Age })
	ages := ""
	for _, p := range us {
		ages += fmt.Sprint(p.Age)
	}
	fmt.Println(ages)
	f := []float64{3, -1, 2, 2, 0, -0.5, 1e9}
	sort.Float64s(f)
	fmt.Println(f)
	s := []string{"b", "", "a", "ab", "B", "aa"}
	sort.Strings(s)
	fmt.Printf("%q\n", s)
	idx, found := slices.BinarySearch(s, "ab")
	fmt.Println(idx, found, sort.SearchStrings(s, "zz"))
	r := []int{5, 2, 8}
	sort.Sort(sort.Reverse(sort.IntSlice(r)))
	fmt.Println(r)
}
