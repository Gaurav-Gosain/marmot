package main

import (
	"errors"
	"fmt"
)

type MyErr struct{ Code int }

func (e *MyErr) Error() string { return fmt.Sprint("code ", e.Code) }

var ErrBase = errors.New("base")

func main() {
	e1 := errors.New("one")
	e2 := errors.New("one")
	println(e1 == e2, e1.Error(), e1 == e1)
	w := fmt.Errorf("wrap: %w", ErrBase)
	println(w.Error(), errors.Is(w, ErrBase), errors.Unwrap(w) == ErrBase)
	w2 := fmt.Errorf("outer: %w", w)
	println(errors.Is(w2, ErrBase), errors.Is(w2, e1))
	var me *MyErr
	t := fmt.Errorf("ctx: %w", &MyErr{7})
	println(errors.As(t, &me), me.Code)
	j := errors.Join(e1, ErrBase)
	println(errors.Is(j, ErrBase), errors.Is(j, e1))
	println(j.Error())
	println(errors.Unwrap(e1) == nil, errors.Join(nil, nil) == nil)
}
