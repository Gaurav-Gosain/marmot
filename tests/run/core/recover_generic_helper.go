// A generic function deferred directly is the deferred function itself, not
// a wrapper: recover in a helper it calls (directly, through a func value or
// an interface) returns nil, as gc.
package main

import "fmt"

func helper() { fmt.Println("helper recover nil:", recover() == nil) }

type H struct{}

func (H) Help() { fmt.Println("iface helper recover nil:", recover() == nil) }

type I interface{ Help() }

func G[T any]() { helper() }

func GV[T any](f func()) { f() }

func GI[T any](i I) { i.Help() }

func run(name string, f func()) {
	defer func() { fmt.Println(name, "outer caught:", recover()) }()
	f()
}

func main() {
	run("generic calls helper", func() { defer G[int](); panic("x") })
	run("generic calls func value", func() { defer GV[int](helper); panic("y") })
	run("generic calls interface", func() { defer GI[int](H{}); panic("z") })
}
