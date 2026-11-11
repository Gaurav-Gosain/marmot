package main

import "sort"

type kv struct {
	k, v int
}

func main() {
	var xs []kv
	for i := 0; i < 500; i++ {
		xs = append(xs, kv{i % 7, i})
	}
	sort.SliceStable(xs, func(i, j int) bool { return xs[i].k < xs[j].k })
	ok := true
	for i := 1; i < len(xs); i++ {
		if xs[i-1].k == xs[i].k && xs[i-1].v > xs[i].v {
			ok = false
		}
		if xs[i-1].k > xs[i].k {
			ok = false
		}
	}
	println(ok, xs[0].v, xs[499].v)
}
