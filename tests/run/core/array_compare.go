package main

type K struct {
	A int
	B string
}

func main() {
	a := [2]K{{1, "x"}, {2, "y"}}
	b := [2]K{{1, "x"}, {2, "y"}}
	println(a == b)
	b[1].B = "z"
	println(a == b)
	var i1, i2 [2]interface{}
	i1[0], i2[0] = 1, 1
	println(i1 == i2)
	i2[1] = "s"
	println(i1 == i2)
	f1 := [2]float64{0, 1}
	f2 := [2]float64{-0.0, 1}
	println(f1 == f2)
}
