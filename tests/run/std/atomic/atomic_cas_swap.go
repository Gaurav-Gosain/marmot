package main

import "sync/atomic"

func main() {
	var x int64 = 5
	println(atomic.CompareAndSwapInt64(&x, 5, 10), x, atomic.CompareAndSwapInt64(&x, 5, 20), x)
	println(atomic.SwapInt64(&x, 99), x)
	var u uint32 = 1
	println(atomic.CompareAndSwapUint32(&u, 1, 2), atomic.SwapUint32(&u, 7), u)
	var p uintptr = 3
	println(atomic.AddUintptr(&p, 4), atomic.LoadUintptr(&p))
	atomic.StoreInt64(&x, -1)
	atomic.StoreUint32(&u, 1<<31)
	println(x, u)
	println(atomic.AndInt32(new(int32), 0), atomic.OrUint32(&u, 1), u)
}
