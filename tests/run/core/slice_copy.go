package main

func main() {
	a := []int{1, 2, 3, 4, 5}
	b := make([]int, 3)
	n := copy(b, a)
	println(n, b[0], b[2])
	n = copy(a, a[2:])
	println(n, a[0], a[1], a[2], a[3], a[4])
	a = []int{1, 2, 3, 4, 5}
	n = copy(a[2:], a)
	println(n, a[0], a[1], a[2], a[3], a[4])
	n = copy(a, []int{})
	println(n)
	var nl []int
	println(copy(nl, a), copy(a, nl))
	c := make([]byte, 2)
	println(copy(c, "xyz"), string(c))
}
