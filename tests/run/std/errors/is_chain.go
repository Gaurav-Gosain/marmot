package main

import "errors"

var A = errors.New("a")
var B = errors.New("b")

type wrap struct{ err error }

func (w wrap) Error() string { return "wrap(" + w.err.Error() + ")" }
func (w wrap) Unwrap() error { return w.err }

func main() {
	e := wrap{wrap{A}}
	println(errors.Is(e, A), errors.Is(e, B), e.Error())
	println(errors.Is(nil, A), errors.Is(A, nil), errors.Is(nil, nil))
	println(errors.Unwrap(e) == wrap{A}, errors.Unwrap(A) == nil)
}
