// Declarations of the C runtime core (runtime/core/include/rt.h).
//
// Each function is body-less and bound to its C symbol with //go:linkname,
// so the compiler emits a plain external declaration with the C calling
// convention. Type mapping: int32_t is int32, intptr_t is int, uintptr_t is
// uintptr, any pointer is unsafe.Pointer. A C function pointer parameter is
// passed as unsafe.Pointer holding a code address (see funcPC).
//
// This file is compiled only by marmot. lib/hosttest does not include it.

package runtime

import "unsafe"

// rtMutex matches rt_mutex. The zero value is unlocked.
type rtMutex struct{ state uint32 }

// rtMemstats matches rt_memstats.
type rtMemstats struct {
	heapAlloc    uint64
	heapSys      uint64
	totalAlloc   uint64
	mallocs      uint64
	frees        uint64
	numGC        uint64
	pauseTotalNs uint64
}

// Allocation kinds for rt_alloc.
const (
	rtAllocNoscan       = 0
	rtAllocConservative = 1
	rtAllocPrecise      = 2
)

const rtJmpbufWords = 32

// Process.

//go:linkname rt_exit rt_exit
func rt_exit(code int32)

//go:linkname rt_fatal rt_fatal
func rt_fatal(msg unsafe.Pointer, n uintptr)

//go:linkname rt_argc rt_argc
func rt_argc() int32

//go:linkname rt_argv rt_argv
func rt_argv() unsafe.Pointer

//go:linkname rt_envp rt_envp
func rt_envp() unsafe.Pointer

// Memory.

//go:linkname rt_alloc rt_alloc
func rt_alloc(size uintptr, kind uint32, ptrmap unsafe.Pointer, elemsize uintptr) unsafe.Pointer

//go:linkname rt_gc_collect rt_gc_collect
func rt_gc_collect()

//go:linkname rt_gc_add_roots rt_gc_add_roots
func rt_gc_add_roots(start, end unsafe.Pointer)

//go:linkname rt_gc_set_finalizer rt_gc_set_finalizer
func rt_gc_set_finalizer(obj unsafe.Pointer, fn unsafe.Pointer)

//go:linkname rt_gc_stats rt_gc_stats
//go:noescape
func rt_gc_stats(out *rtMemstats)

//go:linkname rt_gc_set_percent rt_gc_set_percent
func rt_gc_set_percent(percent int32) int32

// Goroutines.

//go:linkname rt_go rt_go
func rt_go(fn unsafe.Pointer, arg unsafe.Pointer)

//go:linkname rt_current rt_current
func rt_current() unsafe.Pointer

//go:linkname rt_gosched rt_gosched
func rt_gosched()

//go:linkname rt_yield rt_yield
func rt_yield()

//go:linkname rt_can_spin rt_can_spin
func rt_can_spin(iter int32) int32

//go:linkname rt_procyield rt_procyield
func rt_procyield(n int32)

//go:linkname rt_park rt_park
func rt_park(unlock unsafe.Pointer, lock unsafe.Pointer)

//go:linkname rt_ready rt_ready
func rt_ready(g unsafe.Pointer)

//go:linkname rt_goexit rt_goexit
func rt_goexit()

//go:linkname rt_gslot rt_gslot
func rt_gslot() *unsafe.Pointer

//go:linkname rt_gparam rt_gparam
func rt_gparam(g unsafe.Pointer) *unsafe.Pointer

//go:linkname rt_goid rt_goid
func rt_goid() int64

//go:linkname rt_numgoroutine rt_numgoroutine
func rt_numgoroutine() int

//go:linkname rt_gomaxprocs rt_gomaxprocs
func rt_gomaxprocs(n int32) int32

//go:linkname rt_numcpu rt_numcpu
func rt_numcpu() int32

// Locks.

//go:linkname rt_lock rt_lock
func rt_lock(m *rtMutex)

//go:linkname rt_unlock rt_unlock
func rt_unlock(m *rtMutex)

// Time.

//go:linkname rt_nanotime rt_nanotime
func rt_nanotime() int64

//go:linkname rt_walltime rt_walltime
//go:noescape
func rt_walltime(sec *int64, nsec *int32)

//go:linkname rt_sleep rt_sleep
func rt_sleep(ns int64)

//go:linkname rt_timer_start rt_timer_start
func rt_timer_start(when int64, fn unsafe.Pointer, arg unsafe.Pointer) unsafe.Pointer

//go:linkname rt_timer_stop rt_timer_stop
func rt_timer_stop(timer unsafe.Pointer) int32

// Raw I/O.

//go:linkname rt_write rt_write
//go:noescape
func rt_write(fd int32, buf unsafe.Pointer, n uintptr) int

//go:linkname rt_read rt_read
//go:noescape
func rt_read(fd int32, buf unsafe.Pointer, n uintptr) int

//go:linkname rt_open rt_open
func rt_open(path unsafe.Pointer, flags int32, mode uint32) int

//go:linkname rt_close rt_close
func rt_close(fd int32) int

//go:linkname rt_syscall rt_syscall
func rt_syscall(num, a1, a2, a3, a4, a5, a6 uintptr) int

//go:linkname rt_entersyscall rt_entersyscall
func rt_entersyscall()

//go:linkname rt_exitsyscall rt_exitsyscall
func rt_exitsyscall()

// Panic support.

//go:linkname rt_setjmp rt_setjmp
func rt_setjmp(buf unsafe.Pointer) int32

//go:linkname rt_longjmp rt_longjmp
func rt_longjmp(buf unsafe.Pointer, val int32)

// Faults.

//go:linkname rt_set_sigpanic rt_set_sigpanic
func rt_set_sigpanic(fn unsafe.Pointer)

// Signals for os/signal.

//go:linkname rt_signal_enable rt_signal_enable
func rt_signal_enable(sig int32) int32

//go:linkname rt_signal_disable rt_signal_disable
func rt_signal_disable(sig int32) int32

//go:linkname rt_signal_ignore rt_signal_ignore
func rt_signal_ignore(sig int32) int32

//go:linkname rt_signal_ignored rt_signal_ignored
func rt_signal_ignored(sig int32) int32

//go:linkname rt_signal_recv rt_signal_recv
func rt_signal_recv() int32

// C library functions. LLVM knows these by name and expands small constant
// sized calls inline.

//go:linkname libc_memmove memmove
func libc_memmove(dst, src unsafe.Pointer, n uintptr) unsafe.Pointer

//go:linkname libc_memset memset
func libc_memset(p unsafe.Pointer, c int32, n uintptr) unsafe.Pointer

//go:linkname libc_memcmp memcmp
func libc_memcmp(a, b unsafe.Pointer, n uintptr) int32

// dlInfo is the C library's Dl_info.
type dlInfo struct {
	fname unsafe.Pointer // file of the image
	fbase uintptr
	sname unsafe.Pointer // nearest symbol, or nil
	saddr uintptr
}

//go:linkname libc_dladdr dladdr
func libc_dladdr(addr uintptr, info *dlInfo) int32

// cSymbol names a code address outside the pc table that lies in a shared
// library (a C library function such as memmove) from the dynamic linker's
// symbol tables, or returns "". Addresses in the executable itself get no
// name: it exports only main, which dladdr would give for all of them.
func cSymbol(pc uintptr) string {
	var info dlInfo
	if libc_dladdr(pc, &info) == 0 || info.sname == nil || info.fbase == exeBase() {
		return ""
	}
	return cstring(info.sname)
}

var exeBaseCache uintptr

// exeBase is the load address of the executable.
func exeBase() uintptr {
	if exeBaseCache == 0 {
		f := cSymbol
		var info dlInfo
		if libc_dladdr(uintptr(funcPC(unsafe.Pointer(&f))), &info) != 0 {
			exeBaseCache = info.fbase
		}
	}
	return exeBaseCache
}

// funcPC returns the code address of the func value stored at fv: the first
// word of the closure object. Use it only on top-level functions whose
// signature is C compatible (pointer and integer parameters), so that a C
// caller can call the code address without the hidden closure parameter.
//
//	f := gostart
//	rt_go(funcPC(unsafe.Pointer(&f)), arg)
func funcPC(fv unsafe.Pointer) unsafe.Pointer {
	return **(**unsafe.Pointer)(fv)
}

//go:linkname rt_pcln rt_pcln
//go:noescape
func rt_pcln(table *unsafe.Pointer, n *int, base *uintptr) int32
