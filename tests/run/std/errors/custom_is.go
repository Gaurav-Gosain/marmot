package main

import "errors"

type E struct{ n int }

func (e E) Error() string { return "e" }
func (e E) Is(t error) bool {
	o, ok := t.(E)
	return ok && (o.n == 0 || o.n == e.n)
}

func main() {
	println(errors.Is(E{5}, E{}), errors.Is(E{5}, E{5}), errors.Is(E{5}, E{6}))
	println(errors.Is(errors.New("x"), E{}))
}
