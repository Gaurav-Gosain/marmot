package main

import "errors"

var (
	ErrA = errors.New("A")
	ErrB = errors.New("B")
)

func classify(err error) string {
	switch {
	case err == nil:
		return "nil"
	case errors.Is(err, ErrA):
		return "a"
	case errors.Is(err, ErrB):
		return "b"
	}
	return "other"
}

type w struct{ e error }

func (x w) Error() string { return "w" }
func (x w) Unwrap() error { return x.e }

func main() {
	for _, e := range []error{nil, ErrA, w{ErrB}, errors.New("A"), w{w{ErrA}}} {
		println(classify(e))
	}
}
