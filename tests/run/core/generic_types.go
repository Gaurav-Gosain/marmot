package main

type Stack[T any] struct{ items []T }

func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	v := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v, true
}
func (s Stack[T]) Len() int { return len(s.items) }

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

type List[T any] struct {
	head *node[T]
	n    int
}

type node[T any] struct {
	v    T
	next *node[T]
}

func (l *List[T]) Add(v T) {
	l.head = &node[T]{v, l.head}
	l.n++
}

func (l *List[T]) Each(f func(T)) {
	for n := l.head; n != nil; n = n.next {
		f(n.v)
	}
}

func main() {
	var s Stack[int]
	s.Push(1)
	s.Push(2)
	v, ok := s.Pop()
	println(v, ok, s.Len())
	s.Pop()
	v, ok = s.Pop()
	println(v, ok)
	ss := &Stack[string]{}
	ss.Push("x")
	str, _ := ss.Pop()
	println(str)
	p := Pair[string, int]{"a", 1}
	q := Pair[Pair[int, int], bool]{Pair[int, int]{1, 2}, true}
	println(p.Key, p.Val, q.Key.Val, q.Val)
	var l List[float64]
	l.Add(1.5)
	l.Add(2.5)
	t := 0.0
	l.Each(func(f float64) { t += f })
	println(t, l.n)
}
