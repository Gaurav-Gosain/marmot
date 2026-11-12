// A pointer to a composite literal stored into another literal that is
// passed to a call: the inner literal must be complete before the call.
package main

import "fmt"

type step struct {
	name string
	a, b int
}

type wrap struct{ p *step }

type iface interface{ get() *step }

func (w wrap) get() *step { return w.p }

func use(i iface) { s := i.get(); fmt.Println(s.name, s.a, s.b) }

func name(x int) string { return fmt.Sprint("n", x) }

func run(x, y int) {
	x, y = x+1, y+1
	use(wrap{&step{name(x), x, y}})
}

func main() {
	run(1, 2)
	run(10, 20)
}
