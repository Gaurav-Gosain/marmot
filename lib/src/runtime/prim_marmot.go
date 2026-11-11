// Primitives for the pure runtime files, implemented on the C core.
//
// The pure files (print.go, string.go, slice.go, alg.go, map.go, chan.go,
// select.go, ...) use only these functions and types to touch memory,
// locks, goroutines and output. lib/hosttest/runtime provides the same set
// on top of the host Go runtime, so the pure files are tested there.
//
// Primitive set:
//
//	type mutex
//	lock(*mutex), unlock(*mutex)
//	mallocgc(size uintptr, typ *_type) unsafe.Pointer
//	mallocConservative(size uintptr) unsafe.Pointer
//	memmove(dst, src unsafe.Pointer, n uintptr)
//	memclrNoHeapPointers(p unsafe.Pointer, n uintptr)
//	memequal(a, b unsafe.Pointer, n uintptr) bool
//	memcmp(a, b unsafe.Pointer, n uintptr) int
//	writeErr(p unsafe.Pointer, n uintptr)
//	getg() *g
//	gopark(unlockf func(unsafe.Pointer), lock unsafe.Pointer)
//	goready(gp *g)
//	throw(s string)
//	exit(code int32)
//	nanotime() int64
//	atomicLoad32, atomicStore32, atomicCas32, atomicXadd64
//	appendFloat(dst []byte, v float64, bitSize int) []byte
//	appendComplex(dst []byte, c complex128, bitSize int) []byte
//
// Compiled only by marmot.

package runtime

import (
	"internal/runtime/atomic"
	"internal/strconv"
	"unsafe"
)

type mutex struct{ m rtMutex }

func lock(l *mutex)   { rt_lock(&l.m) }
func unlock(l *mutex) { rt_unlock(&l.m) }

// zerobase is the address of all zero-sized allocations.
var zerobase uintptr

// mallocgc allocates size bytes of zeroed memory. typ describes one element:
// size must be a multiple of typ.size. A nil typ means the memory holds no
// pointers.
func mallocgc(size uintptr, typ *_type) unsafe.Pointer {
	if size == 0 {
		return unsafe.Pointer(&zerobase)
	}
	if typ == nil || typ.ptrdata == 0 {
		return rt_alloc(size, rtAllocNoscan, nil, 0)
	}
	if typ.gcdata == nil {
		return rt_alloc(size, rtAllocConservative, nil, 0)
	}
	return rt_alloc(size, rtAllocPrecise, unsafe.Pointer(typ.gcdata), typ.size)
}

// mallocConservative allocates size bytes that are scanned conservatively.
// Used for runtime structures that hold pointers and have no descriptor.
func mallocConservative(size uintptr) unsafe.Pointer {
	return rt_alloc(size, rtAllocConservative, nil, 0)
}

func memmove(dst, src unsafe.Pointer, n uintptr) {
	if n != 0 {
		libc_memmove(dst, src, n)
	}
}

func memclrNoHeapPointers(p unsafe.Pointer, n uintptr) {
	if n != 0 {
		libc_memset(p, 0, n)
	}
}

func memequal(a, b unsafe.Pointer, n uintptr) bool {
	if a == b || n == 0 {
		return true
	}
	return libc_memcmp(a, b, n) == 0
}

func memcmp(a, b unsafe.Pointer, n uintptr) int {
	if n == 0 {
		return 0
	}
	return int(libc_memcmp(a, b, n))
}

func writeErr(p unsafe.Pointer, n uintptr) {
	for n > 0 {
		r := rt_write(2, p, n)
		if r <= 0 {
			return
		}
		p = unsafe.Add(p, r)
		n -= uintptr(r)
	}
}

var goidNext uint64

func getg() *g {
	slot := rt_gslot()
	if *slot != nil {
		return (*g)(*slot)
	}
	gp := (*g)(mallocConservative(unsafe.Sizeof(g{})))
	gp.handle = rt_current()
	gp.goid = int64(atomicXadd64(&goidNext, 1))
	gp.rand = uint64(rt_nanotime()) ^ uint64(uintptr(unsafe.Pointer(gp)))
	*slot = unsafe.Pointer(gp)
	return gp
}

// gopark parks the current goroutine. unlockf(lock) runs after the
// goroutine is off its stack. unlockf must be a top-level function.
func gopark(unlockf func(unsafe.Pointer), lock unsafe.Pointer) {
	if unlockf == nil {
		rt_park(nil, lock)
		return
	}
	rt_park(funcPC(unsafe.Pointer(&unlockf)), lock)
}

func goready(gp *g) {
	rt_ready(gp.handle)
}

// throw reports a fatal runtime error and exits with status 2. It cannot
// be recovered.
func throw(s string) {
	rt_fatal(unsafe.Pointer(unsafe.StringData(s)), uintptr(len(s)))
}

func exit(code int32) {
	rt_exit(code)
}

func nanotime() int64 {
	return rt_nanotime()
}

func atomicLoad32(p *uint32) uint32               { return atomic.Load(p) }
func atomicStore32(p *uint32, v uint32)           { atomic.Store(p, v) }
func atomicCas32(p *uint32, old, new uint32) bool { return atomic.Cas(p, old, new) }
func atomicXadd64(p *uint64, d int64) uint64      { return atomic.Xadd64(p, d) }

func appendFloat(dst []byte, v float64, bitSize int) []byte {
	return strconv.AppendFloat(dst, v, 'g', -1, bitSize)
}

func appendComplex(dst []byte, c complex128, bitSize int) []byte {
	return strconv.AppendComplex(dst, c, 'g', -1, bitSize)
}
