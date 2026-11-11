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
//	goparkunlock(l *mutex), goparkselect(gp *g), goparkForever()
//	goready(gp *g)
//	throw(s string)
//	exit(code int32)
//	nanotime() int64
//	atomicLoad32, atomicStore32, atomicCas32, atomicXadd32, atomicXadd64,
//	atomicLoadp, atomicStorep
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
func rt_mem_equal(a, b unsafe.Pointer, n int) int32

//go:linkname rt_compare rt_compare
func rt_compare(a unsafe.Pointer, an int, b unsafe.Pointer, bn int) int32

// memequal compares n bytes with the C core's SIMD primitive, which LLVM
// inlines from rtcore.bc.
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

func getg() *g {
	slot := rt_gslot()
	if *slot != nil {
		return (*g)(*slot)
	}
	gp := (*g)(mallocConservative(unsafe.Sizeof(g{})))
	gp.handle = rt_current()
	gp.goid = rt_goid()
	gp.rand = uint64(rt_nanotime()) ^ uint64(uintptr(unsafe.Pointer(gp)))
	*slot = unsafe.Pointer(gp)
	return gp
}

// Park primitives. The C core calls the unlock callback by code address;
// the addresses are computed once at init (taking the address of a func
// variable on every park would allocate).
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
	tf := timerFire
	timerFirePC = funcPC(unsafe.Pointer(&tf))
	cr := coroReadyCommit
	coroReadyCommitPC = funcPC(unsafe.Pointer(&cr))
	cs := corostart
	corostartPC = funcPC(unsafe.Pointer(&cs))
}

// goparkunlock parks the current goroutine and unlocks l once it is off
// its stack, so a waker that takes l cannot miss the park.
func goparkunlock(l *mutex) {
	rt_park(parkunlockPC, unsafe.Pointer(l))
}

// goparkselect parks the current goroutine and unlocks the channels of its
// select (gp.waiting) once it is off its stack.
func goparkselect(gp *g) {
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
func atomicXadd32(p *uint32, d int32) uint32           { return atomic.Xadd(p, d) }
func atomicXadd64(p *uint64, d int64) uint64           { return atomic.Xadd64(p, d) }

//go:linkname rt_hash_bytes rt_hash_bytes
func rt_hash_bytes(p unsafe.Pointer, n int, seed uint64) uint64

// hashBytes hashes n bytes at p with the C core's SIMD wyhash.
func hashBytes(p unsafe.Pointer, n uintptr, seed uintptr) uintptr {
	return uintptr(rt_hash_bytes(p, int(n), uint64(seed^hashkey)))
}
