package main

func main() {
	s := []int{1, 2, 3, 4, 5}
	println(len(s), cap(s), s[0], s[4])
	t := s[1:3]
	println(len(t), cap(t), t[0], t[1])
	u := s[:0]
	println(len(u), cap(u))
	v := s[2:]
	println(len(v), cap(v))
	w := s[1:3:4]
	println(len(w), cap(w))
	var n []int
	println(len(n), cap(n), n == nil)
	e := []int{}
	println(len(e), cap(e), e == nil)
	m := make([]int, 3, 10)
	println(len(m), cap(m), m[2])
	m = m[:10]
	println(len(m))
	z := s[5:]
	println(len(z), cap(z))
}
