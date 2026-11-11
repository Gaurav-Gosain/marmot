package main

type S struct{ a, b int }

func main() {
	m := map[string]S{"k": {1, 2}}
	s := m["k"]
	s.a = 100
	println(m["k"].a, s.a)
	m["k"] = s
	println(m["k"].a)
	a := map[string]int{"x": 1}
	b := a
	b["x"] = 2
	b["y"] = 3
	println(a["x"], len(a))
	var f func(map[int]int)
	f = func(m map[int]int) { m[1] = 1 }
	mm := map[int]int{}
	f(mm)
	println(len(mm))
	x, y := map[int]int{}, map[int]int(nil)
	println(x == nil, y == nil, len(x), len(y))
}
