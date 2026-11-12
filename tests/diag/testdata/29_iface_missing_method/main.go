package main

type Shape interface {
	Area() float64
	Perimeter() float64
}

type Rect struct{ w, h float64 }

func (r Rect) Area() float64 { return r.w * r.h }

func show(s Shape) { println(s.Area()) }

func main() {
	show(Rect{1, 2})
}
