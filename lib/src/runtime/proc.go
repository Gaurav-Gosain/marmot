// Allocation and goroutine entry points, and the public runtime API that
// sits directly on the C core. Compiled only by marmot.

package runtime

import "unsafe"

// init runs before every other package init (the compiler calls it
// first).
func init() {
	initParkPCs()
	hashkey ^= uintptr(rt_nanotime()) * 0x9e3779b97f4a7c15
	sp := sigpanic
	rt_set_sigpanic(funcPC(unsafe.Pointer(&sp)))
}

// sigpanic is called by the C core on the faulting goroutine for a memory
// fault in Go code, as if the faulting instruction had called it.
func sigpanic(addr uintptr, kind int32) {
	if kind == 1 {
		panicmem()
	}
	printlock()
	printstring("unexpected fault address ")
	printhex(uint64(addr))
	printstring("\n")
	printunlock()
	throw("fault")
}

// newobject implements new(T) and heap allocation of escaping variables.
func newobject(t *_type) unsafe.Pointer {
	return mallocgc(t.size, t)
}

// newarray allocates n elements of type t.
func newarray(t *_type, n int) unsafe.Pointer {
	if n == 1 {
		return mallocgc(t.size, t)
	}
	mem, overflow := mulUintptr(t.size, uintptr(n))
	if overflow || mem > maxAlloc || n < 0 {
		panic(plainError("runtime: allocation size out of range"))
	}
	return mallocgc(mem, t)
}

// newproc implements `go f(args)`. The compiler wraps the call in a func()
// closure and passes it here.
func newproc(fn func()) {
	if fn == nil {
		panic(errorString("invalid memory address or nil pointer dereference"))
	}
	rt_go(gostartPC, *(*unsafe.Pointer)(unsafe.Pointer(&fn)))
}

// gostart is the C entry of a new goroutine. arg is the closure object of
// the func() passed to newproc.
func gostart(arg unsafe.Pointer) {
	var fn func()
	*(*unsafe.Pointer)(unsafe.Pointer(&fn)) = arg
	fn()
}

// Gosched yields the processor, allowing other goroutines to run.
func Gosched() {
	rt_gosched()
}

// GC runs a garbage collection.
func GC() {
	rt_gc_collect()
}

// NumGoroutine returns the number of goroutines that currently exist.
func NumGoroutine() int {
	return rt_numgoroutine()
}

// GOMAXPROCS sets the maximum number of CPUs that can be executing
// simultaneously and returns the previous setting. n < 1 does not change it.
func GOMAXPROCS(n int) int {
	return int(rt_gomaxprocs(int32(n)))
}

// NumCPU returns the number of logical CPUs usable by the current process.
func NumCPU() int {
	return int(rt_numcpu())
}

// KeepAlive marks its argument as currently reachable.
//
//go:noinline
func KeepAlive(x any) {
	keepAliveSink = x
	keepAliveSink = nil
}

var keepAliveSink any

// nanotime1 and walltime give the time package its clocks.
func walltime() (sec int64, nsec int32) {
	rt_walltime(&sec, &nsec)
	return
}
