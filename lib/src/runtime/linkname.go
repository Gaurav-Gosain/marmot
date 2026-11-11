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
func sync_runtime_Semrelease(addr *uint32, handoff bool, skipframes int) { semrelease1(addr) }

//go:linkname internal_sync_runtime_SemacquireMutex internal/sync.runtime_SemacquireMutex
func internal_sync_runtime_SemacquireMutex(addr *uint32, lifo bool, skipframes int) {
	semacquire1(addr, lifo)
}

//go:linkname internal_sync_runtime_Semrelease internal/sync.runtime_Semrelease
func internal_sync_runtime_Semrelease(addr *uint32, handoff bool, skipframes int) {
	semrelease1(addr)
}

//go:linkname internal_sync_runtime_canSpin internal/sync.runtime_canSpin
func internal_sync_runtime_canSpin(i int) bool { return false }

//go:linkname internal_sync_runtime_doSpin internal/sync.runtime_doSpin
func internal_sync_runtime_doSpin() {}

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

// internal/poll. File descriptors are not registered with a poller:
// pollOpen fails, so internal/poll uses blocking system calls, which the C
// core runs without stalling other goroutines.

//go:linkname poll_runtime_Semacquire internal/poll.runtime_Semacquire
func poll_runtime_Semacquire(addr *uint32) { semacquire1(addr, false) }

//go:linkname poll_runtime_Semrelease internal/poll.runtime_Semrelease
func poll_runtime_Semrelease(addr *uint32) { semrelease1(addr) }

//go:linkname poll_runtimeNano internal/poll.runtimeNano
func poll_runtimeNano() int64 { return rt_nanotime() }

//go:linkname poll_runtime_pollServerInit internal/poll.runtime_pollServerInit
func poll_runtime_pollServerInit() {}

//go:linkname poll_runtime_pollOpen internal/poll.runtime_pollOpen
func poll_runtime_pollOpen(fd uintptr) (uintptr, int) { return 0, _ENOTSUP }

//go:linkname poll_runtime_pollClose internal/poll.runtime_pollClose
func poll_runtime_pollClose(ctx uintptr) {}

//go:linkname poll_runtime_pollWait internal/poll.runtime_pollWait
func poll_runtime_pollWait(ctx uintptr, mode int) int { return 0 }

//go:linkname poll_runtime_pollWaitCanceled internal/poll.runtime_pollWaitCanceled
func poll_runtime_pollWaitCanceled(ctx uintptr, mode int) {}

//go:linkname poll_runtime_pollReset internal/poll.runtime_pollReset
func poll_runtime_pollReset(ctx uintptr, mode int) int { return 0 }

//go:linkname poll_runtime_pollSetDeadline internal/poll.runtime_pollSetDeadline
func poll_runtime_pollSetDeadline(ctx uintptr, d int64, mode int) {}

//go:linkname poll_runtime_pollUnblock internal/poll.runtime_pollUnblock
func poll_runtime_pollUnblock(ctx uintptr) {}

//go:linkname poll_runtime_isPollServerDescriptor internal/poll.runtime_isPollServerDescriptor
func poll_runtime_isPollServerDescriptor(fd uintptr) bool { return false }

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
