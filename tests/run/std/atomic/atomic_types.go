package main

import "sync/atomic"

func main() {
	var a atomic.Int64
	a.Store(10)
	println(a.Add(5), a.Load(), a.Swap(1), a.CompareAndSwap(1, 2), a.Load())
	var u atomic.Uint32
	println(u.Add(3), u.Add(^uint32(0)))
	var b atomic.Bool
	println(b.Load(), b.Swap(true), b.Load(), b.CompareAndSwap(true, false), b.Load())
	var i atomic.Int32
	i.Store(-5)
	println(i.Load(), i.And(3), i.Or(4), i.Load())
	var up atomic.Uintptr
	up.Store(9)
	println(up.Load())
}
