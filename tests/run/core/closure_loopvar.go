package main

func main() {
	var fs []func() int
	for i := 0; i < 3; i++ {
		fs = append(fs, func() int { return i })
	}
	for _, f := range fs {
		println(f())
	}
	var ps []*int
	for i := 0; i < 3; i++ {
		ps = append(ps, &i)
	}
	println(*ps[0], *ps[1], *ps[2])
	var gs []func() string
	for _, s := range []string{"a", "b", "c"} {
		gs = append(gs, func() string { return s })
	}
	println(gs[0](), gs[1](), gs[2]())
	var hs []func() int
	for i := 0; i < 6; i++ {
		hs = append(hs, func() int { i += 10; return i })
		i++
	}
	println(len(hs), hs[0](), hs[0](), hs[1]())
	var ks []func() int
	for i := range 3 {
		ks = append(ks, func() int { return i })
	}
	println(ks[0](), ks[1](), ks[2]())
}
