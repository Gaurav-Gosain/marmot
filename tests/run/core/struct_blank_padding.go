package main

type S struct {
	A int8
	_ int32
	B int8
}

type T struct {
	X int
	_ [3]byte
	Y int
}

func main() {
	a := S{A: 1, B: 2}
	b := S{A: 1, B: 2}
	println(a == b, a.A, a.B)
	t := T{X: 1, Y: 2}
	println(t.X, t.Y, t == T{X: 1, Y: 2})
	var arr [3]S
	arr[1].B = 5
	println(arr[0].B, arr[1].B)
}
