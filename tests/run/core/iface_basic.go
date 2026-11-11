package main

type Shape interface {
	Area() int
	Name() string
}

type Rect struct{ w, h int }
type Square struct{ s int }

func (r Rect) Area() int       { return r.w * r.h }
func (r Rect) Name() string    { return "rect" }
func (s *Square) Area() int    { return s.s * s.s }
func (s *Square) Name() string { return "square" }

func show(s Shape) { println(s.Name(), s.Area()) }

func main() {
	show(Rect{2, 3})
	show(&Rect{4, 5})
	show(&Square{6})
	shapes := []Shape{Rect{1, 1}, &Square{2}}
	t := 0
	for _, s := range shapes {
		t += s.Area()
	}
	println(t)
	var s Shape = Rect{3, 3}
	r := s.(Rect)
	r.w = 100
	println(s.Area(), r.Area())
}
