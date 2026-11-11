package main

type In struct{ v [3]int }
type Out struct {
	in  In
	arr [2]In
	p   *In
}

func mod(o Out) Out {
	o.in.v[0] = 100
	o.arr[1].v[2] = 200
	o.p.v[1] = 300
	return o
}

func main() {
	shared := &In{}
	a := Out{p: shared}
	b := mod(a)
	println(a.in.v[0], b.in.v[0], a.arr[1].v[2], b.arr[1].v[2], shared.v[1])
	c := a
	c.arr = [2]In{{[3]int{1, 2, 3}}, {}}
	println(a.arr[0].v[0], c.arr[0].v[0])
	arrs := [2][2]Out{}
	brr := arrs
	brr[1][1].in.v[2] = 5
	println(arrs[1][1].in.v[2], brr[1][1].in.v[2])
	var iface interface{} = a
	a.in.v[1] = 77
	println(iface.(Out).in.v[1], a.in.v[1])
	ch := make(chan Out, 1)
	ch <- a
	a.in.v[1] = 78
	println((<-ch).in.v[1])
	ms := map[int]In{1: {}}
	in := ms[1]
	in.v[0] = 4
	println(ms[1].v[0], in.v[0])
	fn := func(i In) { i.v[0] = 9 }
	fn(in)
	println(in.v[0])
}
