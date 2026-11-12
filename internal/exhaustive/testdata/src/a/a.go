package a

type Color int

const (
	Red Color = iota
	Green
	Blue
)

const Crimson = Red // an alias name of the same value

type Weekday string

const (
	Mon Weekday = "mon"
	Tue Weekday = "tue"
)

func colors(c Color) {
	switch c { // want "switch on Color misses Blue"
	case Red, Green:
	}
	switch c {
	case Red, Green, Blue:
	}
	switch c {
	case Red:
	default:
	}
	switch c { // handles one of three: a filter, not reported
	case Red:
	}
	//exhaustive:ignore
	switch c {
	case Red:
	}
	switch c { // want "switch on Color misses Blue"
	case Crimson, Green:
	}
}

func days(d Weekday) {
	switch d { // want "switch on Weekday misses Tue"
	case Mon:
	}
}

// Shape is sealed: only this package implements it.
type Shape interface{ area() float64 }

type Square struct{ s float64 }
type Circle struct{ r float64 }
type Tri struct{ b, h float64 }

func (q Square) area() float64 { return q.s * q.s }
func (c *Circle) area() float64 { return 3 * c.r * c.r }
func (t Tri) area() float64    { return t.b * t.h / 2 }

func shapes(s Shape) {
	switch s.(type) { // want `type switch on Shape misses Tri`
	case Square, *Circle:
	}
	switch v := s.(type) {
	case Square, *Circle, Tri:
		_ = v
	}
}

// Open interfaces are not checked.
type Namer interface{ Name() string }

func open(n Namer) {
	switch n.(type) {
	case nil:
	}
}

func plain(n int) {
	switch n {
	case 1:
	}
}
