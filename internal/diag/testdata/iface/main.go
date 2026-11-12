package main

import "fmt"

type Shape interface {
	Area() float64
	Perimeter() float64
	Name() string
}

type Square struct{ side float64 }

func (s *Square) Area() float64 { return s.side * s.side }

func (s Square) Perimter() float64 { return 4 * s.side }

type Circle struct{ r float64 }

func (c Circle) Area() int          { return 0 }
func (c Circle) Perimeter() float64 { return 0 }
func (c Circle) Name() string       { return "circle" }

func describe(s Shape) { fmt.Println(s.Name(), s.Area()) }

func main() {
	describe(Square{side: 2})
	var s Shape = Circle{}
	_ = s
}
