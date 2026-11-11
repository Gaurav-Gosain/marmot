// generic containers: a stack and a linked list, instantiated at int and a struct.
package main

const n = 3000000

type stack[T any] struct{ items []T }

func (s *stack[T]) push(v T) { s.items = append(s.items, v) }
func (s *stack[T]) pop() T {
	v := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v
}

type list[T any] struct {
	head *elem[T]
	size int
}
type elem[T any] struct {
	v    T
	next *elem[T]
}

func (l *list[T]) add(v T) { l.head = &elem[T]{v, l.head}; l.size++ }
func (l *list[T]) each(f func(T)) {
	for e := l.head; e != nil; e = e.next {
		f(e.v)
	}
}

type pt struct{ x, y int }

func mapSlice[T, U any](in []T, f func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

func main() {
	var si stack[int]
	var sp stack[pt]
	sum := 0
	for r := 0; r < 50; r++ {
		for i := 0; i < n; i++ {
			si.push(i)
			sp.push(pt{i, r})
		}
		for i := 0; i < n; i++ {
			sum += si.pop() + sp.pop().x
		}
	}
	var l list[pt]
	for i := 0; i < n; i++ {
		l.add(pt{i, i * 2})
	}
	l.each(func(p pt) { sum += p.y - p.x })
	xs := make([]int, n)
	for i := range xs {
		xs[i] = i
	}
	ys := mapSlice(xs, func(x int) pt { return pt{x, x + 1} })
	for _, p := range ys {
		sum += p.y
	}
	println("generic", sum, l.size)
}
