// Audit (loops, 2026-11-12): the method value s.Name of a nil interface
// panics with a nil pointer dereference in gc; marmot panics with
// "interface conversion: interface is nil, not main.S" (dev and -release,
// also with -no-devirt, so the frontend method value lowering).
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
