package main

import "errors"

func main() {
	defer func() {
		r := recover()
		err, ok := r.(error)
		println(ok, err.Error())
		println(errors.Is(err, err))
	}()
	panic(errors.New("custom"))
}
