package main

import "errors"

var A = errors.New("a")
var B = errors.New("b")
var C = errors.New("c")

func main() {
	j := errors.Join(A, nil, B)
	println(j.Error())
	println(errors.Is(j, A), errors.Is(j, B), errors.Is(j, C))
	u, ok := j.(interface{ Unwrap() []error })
	println(ok, len(u.Unwrap()))
	jj := errors.Join(j, C)
	println(errors.Is(jj, A), errors.Is(jj, C))
	println(errors.Join() == nil, errors.Join(nil) == nil)
}
