// The method value s.Name of a nil interface panics with a nil pointer
// dereference (as gc), not an interface conversion error.
package main

import "fmt"

type S interface{ Name() string }

func main() {
	defer func() {
		r := recover()
		err, ok := r.(error)
		fmt.Println(ok, err)
	}()
	var s S
	f := s.Name // gc: nil pointer dereference here, before f is called
	fmt.Println("not reached", f != nil)
}
