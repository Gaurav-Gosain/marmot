package main

func main() {
	s := make([]byte, 0)
	last := 0
	for i := 0; i < 5000; i++ {
		s = append(s, 1)
		if cap(s) != last {
			last = cap(s)
			if last >= len(s) {
				println("ok", len(s) <= cap(s))
			}
		}
	}
	println(len(s))
	q := append([]int(nil), make([]int, 5)...)
	println(len(q), cap(q) >= 5)
	r := append(make([]int, 3, 3), 1)
	println(len(r), cap(r) >= 4)
}
