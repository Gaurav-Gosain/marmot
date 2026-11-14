// recover in a function deferred through a func value (g := f; defer g(),
// h := T.M; defer h(t)) stops the panic, as gc: the func value wrapper
// ($fv) is a frame recover must see through.
package main

import "fmt"

type T struct{ n int }

func (t T) M() { fmt.Println("M", recover() != nil) }

func plain() { fmt.Println("plain", recover() != nil) }

func try(name string, f func()) {
	defer func() { recover() }()
	f()
	fmt.Println(name, "no panic")
}

func main() {
	h := T.M
	g := plain
	try("direct method expr", func() { defer T.M(T{1}); panic("e") })
	try("method expr value", func() { defer h(T{1}); panic("e") })
	try("func value", func() { defer g(); panic("e") })
	try("method value", func() { m := T{1}.M; defer m(); panic("e") })
	try("plain", func() { defer plain(); panic("e") })
}
