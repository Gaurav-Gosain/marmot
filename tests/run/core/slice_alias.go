package main

func main() {
	a := []int{1, 2, 3, 4, 5}
	b := a[1:4]
	b[0] = 20
	println(a[1], b[0])
	b = append(b, 99)
	println(a[4], len(b), cap(b))
	b = append(b, 100)
	b[0] = -1
	println(a[1], b[0])
	c := a[:2:2]
	c = append(c, 7)
	println(a[2], c[2])
	d := a[1:2]
	e := a[1:2]
	d[0] = 55
	println(e[0])
}
