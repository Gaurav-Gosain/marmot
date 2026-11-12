package main

import "fmt"

func use(s Store) {
	s.Put("a", "1")
	v, ok := s.Get("a")
	fmt.Println(v, ok)
	s.Delete("a")
	_, ok = s.Get("a")
	fmt.Println(ok)
}

func main() {
	use(NewMem())
}
