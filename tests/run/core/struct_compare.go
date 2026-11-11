package main

type A struct {
	I int
	S string
	F float64
}

type B struct {
	A
	P *int
	I interface{}
}

func main() {
	x := A{1, "a", 1.5}
	y := A{1, "a", 1.5}
	println(x == y)
	y.S = "b"
	println(x == y, x != y)
	n := 5
	b1 := B{x, &n, 7}
	b2 := B{x, &n, 7}
	println(b1 == b2)
	b2.I = "7"
	println(b1 == b2)
	b2.I = 7
	m := 5
	b2.P = &m
	println(b1 == b2)
	type E struct{}
	println(E{} == E{})
	nan := 0.0
	nan = nan / nan
	println(A{F: nan} == A{F: nan})
}
