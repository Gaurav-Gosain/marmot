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
//	mallocPrecise(size uintptr, bits *byte, elemsize uintptr) unsafe.Pointer
//	memmove(dst, src unsafe.Pointer, n uintptr)
//	memclrNoHeapPointers(p unsafe.Pointer, n uintptr)
//	memequal(a, b unsafe.Pointer, n uintptr) bool
//	memcmp(a, b unsafe.Pointer, n uintptr) int
//	writeErr(p unsafe.Pointer, n uintptr)
//	getg() *g
//	goparkunlock(l *mutex), goparkselect(gp *g), goparkForever()
//	goready(gp *g)
//	throw(s string)
//	exit(code int32)
//	nanotime() int64
//	atomicLoad32, atomicStore32, atomicCas32, atomicXadd32, atomicXadd64,
//	atomicLoadp, atomicStorep, atomicLoad64, atomicStore64, atomicCas64,
//	atomicOr64, procyield(n int32)
//	hashBytes(p unsafe.Pointer, n uintptr, seed uintptr) uintptr
//
// Compiled only by marmot.

package runtime

import (
	"internal/runtime/atomic"
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
	if typ.gcdata == nil || typ.size&(ptrSize-1) != 0 {
		// No bitmap, or an element size the precise mode cannot describe.
		return rt_alloc(size, rtAllocConservative, nil, 0)
	}
	return rt_alloc(size, rtAllocPrecise, unsafe.Pointer(typ.gcdata), typ.size)
}

// mallocPrecise allocates size bytes made of size/elemsize elements, each
// described by the static pointer bitmap bits (elemsize a multiple of 8).
func mallocPrecise(size uintptr, bits *byte, elemsize uintptr) unsafe.Pointer {
	return rt_alloc(size, rtAllocPrecise, unsafe.Pointer(bits), elemsize)
}

// mallocNoZero allocates size bytes without pointers and without zeroing
// them; the caller overwrites every byte it exposes (gc's mallocgc with
// needzero false).
func mallocNoZero(size uintptr) unsafe.Pointer {
	if size == 0 {
		return unsafe.Pointer(&zerobase)
	}
	return rt_alloc_nozero(size)
}

//go:linkname rt_alloc_nozero rt_alloc_nozero
func rt_alloc_nozero(size uintptr) unsafe.Pointer

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

//go:linkname rt_mem_equal rt_mem_equal
//go:noescape
func rt_mem_equal(a, b unsafe.Pointer, n int) int32

//go:linkname rt_compare rt_compare
//go:noescape
func rt_compare(a unsafe.Pointer, an int, b unsafe.Pointer, bn int) int32

// memequal compares n bytes with the C core's SIMD primitive, which LLVM
// inlines from the core's bitcode.
func memequal(a, b unsafe.Pointer, n uintptr) bool {
	if a == b || n == 0 {
		return true
	}
	return rt_mem_equal(a, b, int(n)) != 0
}

// memcmp returns the sign of the bytewise comparison of n bytes.
func memcmp(a, b unsafe.Pointer, n uintptr) int {
	if n == 0 {
		return 0
	}
	return int(rt_compare(a, int(n), b, int(n)))
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

// getg returns the Go runtime state of the running goroutine. The C core
// may keep a goroutine's slot when it reuses the goroutine structure, so a
// g whose handle is not the running goroutine belongs to a goroutine that
// has ended: it is reset and reused (keeping its sudog cache and select
// buffer).
func getg() *g {
	slot := rt_gslot()
	cur := rt_current()
	gp := (*g)(*slot)
	if gp != nil && gp.handle == cur {
		return gp
	}
	if gp == nil {
		gp = (*g)(mallocConservative(unsafe.Sizeof(g{})))
		*slot = unsafe.Pointer(gp)
	}
	initg(gp, cur)
	return gp
}

// initg makes gp the state of the goroutine with handle cur: everything
// cleared except the sudog cache and the select buffer, which are reused.
// The C core reuses goroutine structures together with their slot, so
// gostart calls it for every new goroutine even when the handle matches.
func initg(gp *g, cur unsafe.Pointer) {
	sc, so := gp.sudogcache, gp.selorder
	*gp = g{}
	gp.sudogcache, gp.selorder = sc, so
	gp.handle = cur
	gp.goid = rt_goid()
	gp.rand = uint64(rt_nanotime()) ^ uint64(uintptr(unsafe.Pointer(gp)))
}

// resetGoroutineState is called when a goroutine starts.
func resetGoroutineState(gp *g) { initg(gp, gp.handle) }

// Park primitives. The C core calls the unlock callback by code address;
// the addresses are computed once, at init or on first use (taking the
// address of a func variable on every park would allocate).
var (
	parkunlockPC      unsafe.Pointer
	selparkcommitPC   unsafe.Pointer
	gostartPC         unsafe.Pointer
	timerFirePC       unsafe.Pointer
	coroReadyCommitPC unsafe.Pointer
	corostartPC       unsafe.Pointer
)

func initParkPCs() {
	pu := parkunlock
	parkunlockPC = funcPC(unsafe.Pointer(&pu))
	sc := selparkcommit
	selparkcommitPC = funcPC(unsafe.Pointer(&sc))
	gs := gostart
	gostartPC = funcPC(unsafe.Pointer(&gs))
}

// lazyPC returns *pc, computing it with f on first use. The timer and
// coroutine callbacks are set up this way so that a program that uses
// neither does not link them (and the C core code they reach).
func lazyPC(pc *unsafe.Pointer, f func() unsafe.Pointer) unsafe.Pointer {
	if p := atomicLoadp(pc); p != nil {
		return p
	}
	p := f()
	atomicStorep(pc, p)
	return p
}

// goparkunlock parks the current goroutine and unlocks l once it is off
// its stack, so a waker that takes l cannot miss the park.
func goparkunlock(l *mutex) {
	rt_park(parkunlockPC, unsafe.Pointer(l))
}

// goparkunlockDurable is goparkunlock for blocking that testing/synctest
// counts as durable (channels, sync.Cond, WaitGroup.Wait, sleeps) when the
// goroutine is in a bubble.
func goparkunlockDurable(l *mutex) {
	if getg().bubble != nil {
		rt_park_durable(parkunlockPC, unsafe.Pointer(l))
		return
	}
	rt_park(parkunlockPC, unsafe.Pointer(l))
}

// goparkselect parks the current goroutine and unlocks the channels of its
// select (gp.waiting) once it is off its stack.
func goparkselect(gp *g) {
	if gp.bubble != nil {
		rt_park_durable(selparkcommitPC, unsafe.Pointer(gp))
		return
	}
	rt_park(selparkcommitPC, unsafe.Pointer(gp))
}

// goparkForever parks the current goroutine with nothing that will ever
// wake it (select {}, nil channel operations).
func goparkForever() {
	rt_park(nil, nil)
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

func atomicLoad32(p *uint32) uint32                    { return atomic.Load(p) }
func atomicStore32(p *uint32, v uint32)                { atomic.Store(p, v) }
func atomicCas32(p *uint32, old, new uint32) bool      { return atomic.Cas(p, old, new) }
func atomicLoadp(p *unsafe.Pointer) unsafe.Pointer     { return atomic.Loadp(unsafe.Pointer(p)) }
func atomicStorep(p *unsafe.Pointer, v unsafe.Pointer) { atomic.StorepNoWB(unsafe.Pointer(p), v) }
func atomicLoad64(p *uint64) uint64                    { return atomic.Load64(p) }
func atomicStore64(p *uint64, v uint64)                { atomic.Store64(p, v) }
func atomicCas64(p *uint64, old, new uint64) bool      { return atomic.Cas64(p, old, new) }
func atomicOr64(p *uint64, v uint64)                   { atomic.Or64(p, v) }
func atomicXadd32(p *uint32, d int32) uint32           { return atomic.Xadd(p, d) }
func atomicXadd64(p *uint64, d int64) uint64           { return atomic.Xadd64(p, d) }

//go:linkname rt_hash_bytes rt_hash_bytes
//go:noescape
func rt_hash_bytes(p unsafe.Pointer, n int, seed uint64) uint64

// hashBytes hashes n bytes at p with the C core's SIMD wyhash.
func hashBytes(p unsafe.Pointer, n uintptr, seed uintptr) uintptr {
	return uintptr(rt_hash_bytes(p, int(n), uint64(seed^hashkey)))
}

// procyield executes n CPU pause hints.
func procyield(n int32) { rt_procyield(n) }

// canSpin reports whether spin round i (from 0) is worthwhile: more than
// one thread runs goroutines and others are busy (gc's sync_runtime_canSpin).
func canSpin(i int) bool { return rt_can_spin(int32(i)) != 0 }
