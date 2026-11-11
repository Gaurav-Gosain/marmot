package main

import "sync/atomic"

type Cfg struct{ n int }

func main() {
	var p atomic.Pointer[Cfg]
	println(p.Load() == nil)
	c1 := &Cfg{1}
	p.Store(c1)
	println(p.Load().n, p.CompareAndSwap(c1, &Cfg{2}), p.Load().n)
	old := p.Swap(&Cfg{3})
	println(old.n, p.Load().n)
	var v atomic.Value
	println(v.Load() == nil)
	v.Store("hello")
	println(v.Load().(string), v.Swap("bye").(string), v.CompareAndSwap("bye", "again"), v.Load().(string))
	defer func() { println(recover() != nil) }()
	v.Store(5)
}
