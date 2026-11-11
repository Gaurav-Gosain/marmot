package main

import "errors"

type CodeErr struct{ Code int }

func (c CodeErr) Error() string { return "code" }

type PtrErr struct{ Msg string }

func (p *PtrErr) Error() string { return p.Msg }

type wrap struct{ err error }

func (w wrap) Error() string { return "w" }
func (w wrap) Unwrap() error { return w.err }

func main() {
	var ce CodeErr
	err := wrap{CodeErr{404}}
	println(errors.As(err, &ce), ce.Code)
	var pe *PtrErr
	println(errors.As(err, &pe), pe == nil)
	err2 := wrap{&PtrErr{"hello"}}
	println(errors.As(err2, &pe), pe.Msg)
	var target interface{ Error() string }
	println(errors.As(err2, &target))
}
