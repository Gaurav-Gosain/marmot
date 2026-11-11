package main

func modify(a [3]int) [3]int {
	a[0] = 100
	return a
}

func modp(a *[3]int) { a[0] = 100 }

func main() {
	a := [3]int{1, 2, 3}
	b := a
	b[1] = 20
	println(a[1], b[1])
	c := modify(a)
	println(a[0], c[0])
	modp(&a)
	println(a[0])
	println(a == [3]int{100, 2, 3}, a != b)
	var z [5]string
	println(len(z), z[4] == "")
	d := [...]int{5: 1, 2, 1: 7}
	println(len(d), d[5], d[6], d[1], d[0])
	e := [2][2]int{{1, 2}, {3, 4}}
	f := e
	f[1][1] = 0
	println(e[1][1], f[1][1], e == f)
	const n = len(d)
	var g [n]byte
	println(len(g), cap(g))
}
