// recover in a method deferred through a method expression (h := T.M;
// defer h(t)) stops the panic, as gc: the method expression's thunk is a
// compiler wrapper.
package main

import "fmt"

type T struct{ n int }

func (t T) M() {
	if r := recover(); r != nil {
		fmt.Println("M recovered", r, t.n)
	} else {
		fmt.Println("M nil")
	}
}

func run(f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("outer caught", r)
		}
	}()
	f()
}

func main() {
	h := T.M
	defer h(T{4}) // in main: recovers nothing, no panic is left
	run(func() { defer h(T{5}); panic("e") }) // in a closure
}
