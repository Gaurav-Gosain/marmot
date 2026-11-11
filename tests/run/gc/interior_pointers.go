package main

type Big struct {
	pad [100]int
	x   int
	arr [10]*int
}

func make1() *int {
	b := &Big{}
	b.x = 42
	return &b.x
}

func main() {
	p := make1()
	var junk [][]byte
	for i := 0; i < 2000; i++ {
		junk = append(junk, make([]byte, 1000))
		if len(junk) > 10 {
			junk = junk[5:]
		}
	}
	println(*p)
	s := make([]int, 1000)
	for i := range s {
		s[i] = i
	}
	tail := s[990:]
	s = nil
	for i := 0; i < 500; i++ {
		_ = make([]int, 2000)
	}
	println(tail[0], tail[9], len(tail))
}
