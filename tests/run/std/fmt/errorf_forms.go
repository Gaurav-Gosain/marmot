package main

import (
	"errors"
	"fmt"
	"io"
)

func main() {
	e := fmt.Errorf("read failed: %w", io.EOF)
	println(e.Error(), errors.Is(e, io.EOF), errors.Unwrap(e) == io.EOF)
	e2 := fmt.Errorf("code %d: %s", 5, "bad")
	println(e2.Error(), errors.Unwrap(e2) == nil)
	e3 := fmt.Errorf("multi: %w, %w", io.EOF, io.ErrClosedPipe)
	println(e3.Error(), errors.Is(e3, io.ErrClosedPipe))
	var notErr any = "not an error"
	wf := "bad wrap: %w"
	e4 := fmt.Errorf(wf, notErr)
	println(e4.Error())
	e5 := fmt.Errorf("nil wrap: %w", nil)
	println(e5.Error())
	println(fmt.Errorf("%v|%6.2f|%q", []int{1}, 3.14159, "s").Error())
}
