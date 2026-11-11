package main

import "errors"

type T struct{}

func (*T) Error() string { return "t" }

func f() error {
	var t *T
	return t
}

func main() {
	err := f()
	println(err == nil, errors.Is(err, err))
	var nilErr error
	println(errors.Unwrap(nilErr) == nil)
	println(errors.Is(nilErr, nilErr))
	defer func() { println(recover() != nil) }()
	var e error
	_ = e.Error()
}
