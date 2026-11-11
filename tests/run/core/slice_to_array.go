package main

func main() {
	s := []int{1, 2, 3, 4}
	a := [4]int(s)
	a[0] = 99
	println(s[0], a[0])
	p := (*[2]int)(s)
	p[1] = 77
	println(s[1])
	b := [0]int(s[:0])
	println(len(b))
	var nilS []int
	np := (*[0]int)(nilS)
	println(np == nil)
}
