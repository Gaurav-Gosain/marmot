package main

import "sync/atomic"

func main() {
	var i8 atomic.Int32
	i8.Store(1<<31 - 1)
	println(i8.Add(1))
	var u atomic.Uint64
	println(u.Add(^uint64(0)), u.Add(2))
	var i atomic.Int64
	i.Store(-1 << 63)
	println(i.Add(-1))
	var u32 uint32 = 5
	println(atomic.AddUint32(&u32, ^uint32(4)), atomic.AddUint32(&u32, ^uint32(0)))
}
