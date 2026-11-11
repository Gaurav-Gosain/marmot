// Functions that stock standard library packages declare without a body
// and expect the runtime to provide (//go:linkname push from here).
// Compiled only by marmot.

package runtime

import "unsafe"

// sync and internal/sync.

//go:linkname sync_runtime_Semacquire sync.runtime_Semacquire
func sync_runtime_Semacquire(addr *uint32) { semacquire1(addr, false) }

//go:linkname sync_runtime_SemacquireWaitGroup sync.runtime_SemacquireWaitGroup
func sync_runtime_SemacquireWaitGroup(addr *uint32, synctestDurable bool) {
	semacquire1(addr, false)
}

//go:linkname sync_runtime_SemacquireRWMutexR sync.runtime_SemacquireRWMutexR
func sync_runtime_SemacquireRWMutexR(addr *uint32, lifo bool, skipframes int) {
	semacquire1(addr, lifo)
}

//go:linkname sync_runtime_SemacquireRWMutex sync.runtime_SemacquireRWMutex
func sync_runtime_SemacquireRWMutex(addr *uint32, lifo bool, skipframes int) {
	semacquire1(addr, lifo)
}

//go:linkname sync_runtime_Semrelease sync.runtime_Semrelease
func sync_runtime_Semrelease(addr *uint32, handoff bool, skipframes int) {
	if semrelease1(addr) && handoff {
		// Let the woken waiter run now (sync.Mutex starvation mode).
		rt_yield()
	}
}

//go:linkname internal_sync_runtime_SemacquireMutex internal/sync.runtime_SemacquireMutex
func internal_sync_runtime_SemacquireMutex(addr *uint32, lifo bool, skipframes int) {
	semacquire1(addr, lifo)
}

//go:linkname internal_sync_runtime_Semrelease internal/sync.runtime_Semrelease
func internal_sync_runtime_Semrelease(addr *uint32, handoff bool, skipframes int) {
	if semrelease1(addr) && handoff {
		rt_yield()
	}
}

// sync.Mutex spins briefly before parking when the C core says spinning
// can pay off (several threads, others running), as gc does.
//
//go:linkname internal_sync_runtime_canSpin internal/sync.runtime_canSpin
func internal_sync_runtime_canSpin(i int) bool { return rt_can_spin(int32(i)) != 0 }

//go:linkname internal_sync_runtime_doSpin internal/sync.runtime_doSpin
func internal_sync_runtime_doSpin() { rt_procyield(30) }

//go:linkname internal_sync_runtime_nanotime internal/sync.runtime_nanotime
func internal_sync_runtime_nanotime() int64 { return rt_nanotime() }

//go:linkname internal_sync_runtime_rand internal/sync.runtime_rand
func internal_sync_runtime_rand() uint64 { return cheaprand64() }

//go:linkname internal_sync_throw internal/sync.throw
func internal_sync_throw(s string) { throw(s) }

//go:linkname internal_sync_fatal internal/sync.fatal
func internal_sync_fatal(s string) { throw(s) }

//go:linkname sync_throw sync.throw
func sync_throw(s string) { throw(s) }

//go:linkname sync_fatal sync.fatal
func sync_fatal(s string) { throw(s) }

//go:linkname sync_runtime_notifyListAdd sync.runtime_notifyListAdd
func sync_runtime_notifyListAdd(l *notifyList) uint32 { return notifyListAdd(l) }

//go:linkname sync_runtime_notifyListWait sync.runtime_notifyListWait
func sync_runtime_notifyListWait(l *notifyList, t uint32) { notifyListWait(l, t) }

//go:linkname sync_runtime_notifyListNotifyAll sync.runtime_notifyListNotifyAll
func sync_runtime_notifyListNotifyAll(l *notifyList) { notifyListNotifyAll(l) }

//go:linkname sync_runtime_notifyListNotifyOne sync.runtime_notifyListNotifyOne
func sync_runtime_notifyListNotifyOne(l *notifyList) { notifyListNotifyOne(l) }

//go:linkname sync_runtime_notifyListCheck sync.runtime_notifyListCheck
func sync_runtime_notifyListCheck(size uintptr) { notifyListCheck(size) }

//go:linkname sync_runtime_randn sync.runtime_randn
func sync_runtime_randn(n uint32) uint32 { return cheaprandn(n) }

var poolCleanup func()

//go:linkname sync_runtime_registerPoolCleanup sync.runtime_registerPoolCleanup
func sync_runtime_registerPoolCleanup(cleanup func()) { poolCleanup = cleanup }

// procPin pins the goroutine for sync.Pool and atomic.Value. There is one
// logical P, so pinning is a global lock held for a short critical section.
var pinLock mutex

func procPin() int {
	lock(&pinLock)
	return 0
}

func procUnpin() {
	unlock(&pinLock)
}

//go:linkname sync_runtime_procPin sync.runtime_procPin
func sync_runtime_procPin() int { return procPin() }

//go:linkname sync_runtime_procUnpin sync.runtime_procUnpin
func sync_runtime_procUnpin() { procUnpin() }

//go:linkname sync_atomic_runtime_procPin sync/atomic.runtime_procPin
func sync_atomic_runtime_procPin() int { return procPin() }

//go:linkname sync_atomic_runtime_procUnpin sync/atomic.runtime_procUnpin
func sync_atomic_runtime_procUnpin() { procUnpin() }

// internal/poll semaphores and clock. The poller hooks are in netpoll.go.

//go:linkname poll_runtime_Semacquire internal/poll.runtime_Semacquire
func poll_runtime_Semacquire(addr *uint32) { semacquire1(addr, false) }

//go:linkname poll_runtime_Semrelease internal/poll.runtime_Semrelease
func poll_runtime_Semrelease(addr *uint32) { semrelease1(addr) }

//go:linkname poll_runtimeNano internal/poll.runtimeNano
func poll_runtimeNano() int64 { return rt_nanotime() }

// os.

//go:linkname os_runtime_args os.runtime_args
func os_runtime_args() []string { return append([]string{}, goargs()...) }

//go:linkname os_runtime_beforeExit os.runtime_beforeExit
func os_runtime_beforeExit(exitCode int) {}

//go:linkname os_sigpipe os.sigpipe
func os_sigpipe() {}

//go:linkname os_runtime_rand os.runtime_rand
func os_runtime_rand() uint64 { return cheaprand64() }

// syscall.

//go:linkname syscall_runtime_envs syscall.runtime_envs
func syscall_runtime_envs() []string { return append([]string{}, goenvs()...) }

//go:linkname syscall_runtimeSetenv syscall.runtimeSetenv
func syscall_runtimeSetenv(key, value string) {}

//go:linkname syscall_runtimeUnsetenv syscall.runtimeUnsetenv
func syscall_runtimeUnsetenv(key string) {}

//go:linkname syscall_runtimeClearenv syscall.runtimeClearenv
func syscall_runtimeClearenv(env map[string]int) {}

//go:linkname syscall_Getpagesize syscall.Getpagesize
func syscall_Getpagesize() int { return physPageSize }

//go:linkname syscall_Exit syscall.Exit
func syscall_Exit(code int) { rt_exit(int32(code)) }

//go:linkname syscall_runtime_BeforeFork syscall.runtime_BeforeFork
func syscall_runtime_BeforeFork() {}

//go:linkname syscall_runtime_AfterFork syscall.runtime_AfterFork
func syscall_runtime_AfterFork() {}

//go:linkname syscall_runtime_AfterForkInChild syscall.runtime_AfterForkInChild
func syscall_runtime_AfterForkInChild() {}

//go:linkname syscall_runtime_BeforeExec syscall.runtime_BeforeExec
func syscall_runtime_BeforeExec() {}

//go:linkname syscall_runtime_AfterExec syscall.runtime_AfterExec
func syscall_runtime_AfterExec() {}

// maps.

//go:linkname maps_clone maps.clone
func maps_clone(m any) any {
	e := efaceOf(&m)
	src := (*hmap)(e.data)
	t := (*maptype)(unsafe.Pointer(e.typ))
	dst := makemap(t, maplen(src))
	var it hiter
	for mapiterinit(t, src, &it); it.key != nil; mapiternext(&it) {
		typedmemmove(t.elem, mapassign(t, dst, it.key), it.elem)
	}
	e.data = unsafe.Pointer(dst)
	return m
}

// internal/bytealg.

//go:linkname bytealg_MakeNoZero internal/bytealg.MakeNoZero
func bytealg_MakeNoZero(len int) []byte {
	if uint(len) > maxAlloc {
		panicmakeslicelen()
	}
	c := roundupsize(uintptr(len), true)
	return unsafe.Slice((*byte)(mallocgc(c, nil)), c)[:len]
}

// internal/synctest. marmot has no synctest bubbles: nothing is ever
// associated with one.

//go:linkname synctest_Run internal/synctest.Run
func synctest_Run(f func()) { panic(plainError("synctest.Run not supported by marmot")) }

//go:linkname synctest_Wait internal/synctest.Wait
func synctest_Wait() { panic(plainError("synctest.Wait not supported by marmot")) }

//go:linkname synctest_IsInBubble internal/synctest.IsInBubble
func synctest_IsInBubble() bool { return false }

//go:linkname synctest_associate internal/synctest.associate
func synctest_associate(p unsafe.Pointer) int { return 0 }

//go:linkname synctest_disassociate internal/synctest.disassociate
func synctest_disassociate(b unsafe.Pointer) {}

//go:linkname synctest_isAssociated internal/synctest.isAssociated
func synctest_isAssociated(p unsafe.Pointer) bool { return false }

//go:linkname synctest_acquire internal/synctest.acquire
func synctest_acquire() any { return nil }

//go:linkname synctest_release internal/synctest.release
func synctest_release(any) {}

//go:linkname synctest_inBubble internal/synctest.inBubble
func synctest_inBubble(b any, f func()) { f() }

// Functions that standard packages pull by their runtime name
// (//go:linkname local runtime.name).

func rand() uint64              { return cheaprand64() }
func randn(n uint32) uint32     { return cheaprandn(n) }
func fastrand() uint32          { return cheaprand() }
func fastrandn(n uint32) uint32 { return cheaprandn(n) }
func fastrand64() uint64        { return cheaprand64() }

// write is used by internal/godebug.
func write(fd uintptr, p unsafe.Pointer, n int32) int32 {
	return int32(rt_write(int32(fd), p, uintptr(n)))
}

// overflowError and divideError are used by math/bits.
var (
	overflowError = error(errorString("integer overflow"))
	divideError   = error(errorString("integer divide by zero"))
)

// internal/godebug.

//go:linkname godebug_setUpdate internal/godebug.setUpdate
func godebug_setUpdate(update func(string, string)) {
	update("", gogetenv("GODEBUG"))
}

//go:linkname godebug_registerMetric internal/godebug.registerMetric
func godebug_registerMetric(name string, read func() uint64) {}

//go:linkname godebug_setNewIncNonDefault internal/godebug.setNewIncNonDefault
func godebug_setNewIncNonDefault(newIncNonDefault func(string) func()) {}

// runtime/debug.

//go:linkname debug_setGCPercent runtime/debug.setGCPercent
func debug_setGCPercent(in int32) int32 { return rt_gc_set_percent(in) }

//go:linkname debug_freeOSMemory runtime/debug.freeOSMemory
func debug_freeOSMemory() { rt_gc_collect() }

var maxStack = 1 << 30

//go:linkname debug_setMaxStack runtime/debug.setMaxStack
func debug_setMaxStack(in int) int {
	old := maxStack
	maxStack = in
	return old
}

var maxThreads = 10000

//go:linkname debug_setMaxThreads runtime/debug.setMaxThreads
func debug_setMaxThreads(in int) int {
	old := maxThreads
	maxThreads = in
	return old
}

var panicOnFault bool

//go:linkname debug_setPanicOnFault runtime/debug.setPanicOnFault
func debug_setPanicOnFault(new bool) bool {
	old := panicOnFault
	panicOnFault = new
	return old
}

var memoryLimit int64 = 1<<63 - 1

//go:linkname debug_setMemoryLimit runtime/debug.setMemoryLimit
func debug_setMemoryLimit(in int64) int64 {
	old := memoryLimit
	if in >= 0 {
		memoryLimit = in
	}
	return old
}

// debug_readGCStats fills the buffer as the gc runtime lays it out:
// pauses, pause end times, then the time of the last GC, the number of
// GCs and the total pause time. marmot records no individual pauses.
//
//go:linkname debug_readGCStats runtime/debug.readGCStats
func debug_readGCStats(pauses *[]int64) {
	var s rtMemstats
	rt_gc_stats(&s)
	p := (*pauses)[:0]
	p = append(p, 0, int64(s.numGC), int64(s.pauseTotalNs))
	*pauses = p
}

//go:linkname debug_modinfo runtime/debug.modinfo
func debug_modinfo() string { return "" }

//go:linkname debug_WriteHeapDump runtime/debug.WriteHeapDump
func debug_WriteHeapDump(fd uintptr) {}

//go:linkname debug_SetTraceback runtime/debug.SetTraceback
func debug_SetTraceback(level string) {}

// setCrashFD is pulled by runtime/debug.
func setCrashFD(fd uintptr) uintptr { return ^uintptr(0) }

// os/signal, on the C core's signal queue.

//go:linkname signal_enable os/signal.signal_enable
func signal_enable(sig uint32) { rt_signal_enable(int32(sig)) }

//go:linkname signal_disable os/signal.signal_disable
func signal_disable(sig uint32) { rt_signal_disable(int32(sig)) }

//go:linkname signal_ignore os/signal.signal_ignore
func signal_ignore(sig uint32) { rt_signal_ignore(int32(sig)) }

//go:linkname signal_ignored os/signal.signal_ignored
func signal_ignored(sig uint32) bool { return rt_signal_ignored(int32(sig)) != 0 }

//go:linkname signal_recv os/signal.signal_recv
func signal_recv() uint32 { return uint32(rt_signal_recv()) }

//go:linkname signalWaitUntilIdle os/signal.signalWaitUntilIdle
func signalWaitUntilIdle() {}

// crypto. The FIPS service indicator and the bypass flag are
// per-goroutine state in gc; marmot keeps them in g.

//go:linkname fips_fatal crypto/internal/fips140.fatal
func fips_fatal(s string) { throw(s) }

//go:linkname fips_getIndicator crypto/internal/fips140.getIndicator
func fips_getIndicator() uint8 { return getg().fipsIndicator }

//go:linkname fips_setIndicator crypto/internal/fips140.setIndicator
func fips_setIndicator(indicator uint8) { getg().fipsIndicator = indicator }

//go:linkname fips140deps_time_monoTime crypto/internal/fips140deps/time.monoTime
func fips140deps_time_monoTime() int64 { return rt_nanotime() }

//go:linkname sysrand_fatal crypto/internal/sysrand.fatal
func sysrand_fatal(s string) { throw(s) }

//go:linkname fips140_setBypass crypto/fips140.setBypass
func fips140_setBypass() { getg().fipsOnlyBypass = true }

//go:linkname fips140_isBypassed crypto/fips140.isBypassed
func fips140_isBypassed() bool { return getg().fipsOnlyBypass }

//go:linkname fips140_unsetBypass crypto/fips140.unsetBypass
func fips140_unsetBypass() { getg().fipsOnlyBypass = false }

//go:linkname subtle_setDITEnabled crypto/subtle.setDITEnabled
func subtle_setDITEnabled() bool { return false }

//go:linkname subtle_setDITDisabled crypto/subtle.setDITDisabled
func subtle_setDITDisabled() {}

// weak. The collector has no weak references yet, so a weak pointer is a
// strong one: Value never returns nil, which is allowed (the object may
// simply never be collected).

//go:linkname weak_runtime_registerWeakPointer weak.runtime_registerWeakPointer
func weak_runtime_registerWeakPointer(p unsafe.Pointer) unsafe.Pointer {
	h := (*unsafe.Pointer)(mallocConservative(ptrSize))
	*h = p
	return unsafe.Pointer(h)
}

//go:linkname weak_runtime_makeStrongFromWeak weak.runtime_makeStrongFromWeak
func weak_runtime_makeStrongFromWeak(h unsafe.Pointer) unsafe.Pointer {
	return *(*unsafe.Pointer)(h)
}

// crypto/rand and crypto/internal/boring/bcache.

//go:linkname crypto_rand_fatal crypto/rand.fatal
func crypto_rand_fatal(s string) { throw(s) }

// bcache_registerCache would let the collector clear BoringCrypto caches;
// marmot has no BoringCrypto, so the caches are simply kept.
//
//go:linkname bcache_registerCache crypto/internal/boring/bcache.registerCache
func bcache_registerCache(p unsafe.Pointer) {}
