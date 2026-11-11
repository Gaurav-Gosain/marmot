package main

import (
	"errors"
	"fmt"
)

var Base = errors.New("base")

func main() {
	e1 := fmt.Errorf("l1: %w", Base)
	e2 := fmt.Errorf("l2: %w", e1)
	println(e2.Error(), errors.Is(e2, Base))
	e3 := fmt.Errorf("two: %w and %w", Base, errors.New("other"))
	println(e3.Error(), errors.Is(e3, Base))
	e4 := fmt.Errorf("no wrap: %v", Base)
	println(errors.Is(e4, Base), errors.Unwrap(e4) == nil)
	e5 := fmt.Errorf("plain")
	println(e5.Error())
}
