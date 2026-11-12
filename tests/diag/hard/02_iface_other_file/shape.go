package main

type Shape interface {
	Area() float64
	Perimeter() float64
	Name() string
}
