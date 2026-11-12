package main

type Stack struct{ items []int }

func (s *Stack) Push(v int) { s.items = append(s.items, v) }

func main() {
	var s Stack
	s.Puhs(1)
}
