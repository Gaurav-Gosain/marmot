package main

type Square struct{ Side float64 }

func (s *Square) Area() float64      { return s.Side * s.Side }
func (s *Square) Perimeter() float64 { return 4 * s.Side }
func (s *Square) Name() string       { return "square" }
