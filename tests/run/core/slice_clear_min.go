package main

func main() {
	s := []int{1, 2, 3}
	clear(s)
	println(s[0], s[1], s[2], len(s))
	m := map[int]int{1: 1, 2: 2}
	clear(m)
	println(len(m))
	var n []int
	clear(n)
	var nm map[string]int
	clear(nm)
	println(len(n), len(nm))
	b := []bool{true, true}
	clear(b[1:])
	println(b[0], b[1])
}
