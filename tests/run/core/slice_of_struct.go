package main

type P struct{ X, Y int }

func main() {
	ps := []P{{1, 2}, {3, 4}}
	for _, p := range ps {
		p.X = 100
	}
	println(ps[0].X)
	for i := range ps {
		ps[i].X = 100
	}
	println(ps[0].X, ps[1].X)
	pp := &ps[1]
	pp.Y = 7
	println(ps[1].Y)
	ps = append(ps, P{5, 6})
	pp.Y = 8
	println(ps[1].Y, pp.Y)
	ptrs := []*P{{1, 1}, {2, 2}}
	for _, p := range ptrs {
		p.X *= 10
	}
	println(ptrs[0].X, ptrs[1].X)
}
