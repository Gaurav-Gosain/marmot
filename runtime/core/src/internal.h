// internal.h: shared declarations for the runtime core. Not part of the contract.
//
// Thread switching rule: a goroutine can resume on a different OS thread after
// any call that may switch (park, gosched, syscall entry and exit, lock waits).
// Never cache the address of a thread-local variable across such a call. Use
// getm(), which is noinline, to read the current M.

#ifndef MARMOT_INTERNAL_H
#define MARMOT_INTERNAL_H

#include "rt.h"
#include <stdatomic.h>
#include <stdbool.h>

#define RT_API __attribute__((visibility("default")))
#define NOINLINE __attribute__((noinline))
#define INLINE static inline __attribute__((always_inline))
#define LIKELY(x) __builtin_expect(!!(x), 1)
#define UNLIKELY(x) __builtin_expect(!!(x), 0)
#define NORETURN __attribute__((noreturn))
// Conservative scanning reads arbitrary words (stack redzones, padding).
#define NO_SANITIZE __attribute__((no_sanitize("address", "thread", "undefined")))

// ThreadSanitizer needs to know about goroutine switches.
#if defined(__has_feature)
#if __has_feature(thread_sanitizer)
#define RT_TSAN 1
#endif
#endif
#ifdef RT_TSAN
void *__tsan_get_current_fiber(void);
void *__tsan_create_fiber(unsigned flags);
void __tsan_destroy_fiber(void *fiber);
void __tsan_switch_to_fiber(void *fiber, unsigned flags);
#endif

#if defined(__aarch64__)
#define RT_ARCH_ARM64 1
#elif defined(__x86_64__)
#define RT_ARCH_AMD64 1
#else
#error "unsupported architecture"
#endif

#if defined(__APPLE__)
#define RT_OS_DARWIN 1
#elif defined(__linux__)
#define RT_OS_LINUX 1
#else
#error "unsupported OS"
#endif

// Everything declared below is internal to the static library: hidden
// visibility lets the compiler address globals directly instead of through
// the GOT (one load instead of two on every access).
#pragma GCC visibility push(hidden)

typedef struct rt_m rt_m;
typedef struct rt_p rt_p;
typedef struct mcache mcache;
typedef struct mspan mspan;
typedef struct rt_timer rt_timer;

// ---- OS layer (os_darwin.c, os_linux.c) ----

extern uintptr_t rt_pagesize;  // OS page size
void os_init(void);
// Reserve address space with no access. Returns NULL on failure.
void *os_reserve(uintptr_t n);
// Make reserved memory readable and writable. Returns false on failure.
bool os_commit(void *p, uintptr_t n);
// Map fresh zeroed read/write memory. Returns NULL on failure.
void *os_map(uintptr_t n);
void os_unmap(void *p, uintptr_t n);
// Tell the OS the whole pages inside [p, p+n) may be discarded; later reads
// see zero or old data.
void os_release(void *p, uintptr_t n);
void os_protect_none(void *p, uintptr_t n);
int32_t os_ncpu(void);
int64_t os_nanotime(void);
void os_walltime(int64_t *sec, int32_t *nsec);
void os_write_err(const void *buf, uintptr_t n);
void os_exit(int32_t code) NORETURN;
// Futex style wait: sleep while *addr == val, at most ns nanoseconds
// (ns < 0 means forever). Spurious wakeups are allowed.
void os_futex_wait(_Atomic uint32_t *addr, uint32_t val, int64_t ns);
void os_futex_wake(_Atomic uint32_t *addr, bool all);
void os_usleep(uint32_t us);
void os_yield(void);
// Iterate writable data segments of the main executable.
void os_data_segments(void (*fn)(void *start, void *end));
// Thread creation. The new thread runs fn(arg) and exits when it returns.
bool os_thread_create(void (*fn)(void *), void *arg, uintptr_t stacksize);
// Raw system call, returns -errno on failure.
intptr_t os_syscall6(uintptr_t num, uintptr_t a1, uintptr_t a2, uintptr_t a3,
                     uintptr_t a4, uintptr_t a5, uintptr_t a6);

// ---- Small utilities (util.c) ----

void rt_print(const char *s);
void rt_printn(const char *s, uintptr_t n);
void rt_printu(uint64_t v);
void rt_printi(int64_t v);
void rt_printx(uint64_t v);
NORETURN void rt_throw(const char *msg);
// Environment lookup over rt_envp. Returns NULL if unset.
const char *rt_getenv(const char *name);
int64_t rt_atoi(const char *s, bool *ok);
uint64_t rt_fastrand(void);
// Persistent allocation for runtime metadata (never freed, zeroed).
void *persistent_alloc(uintptr_t size, uintptr_t align);

// Fixed-size allocator for runtime structures (spans, Ms, Gs). Not GC managed.
typedef struct fixalloc {
  uintptr_t size;
  void *list;      // free list
  uint8_t *chunk;  // current chunk
  uintptr_t nchunk;
  uint32_t inuse;
} fixalloc;
void fixalloc_init(fixalloc *f, uintptr_t size);
void *fixalloc_alloc(fixalloc *f);  // zeroed
void fixalloc_free(fixalloc *f, void *p);

// ---- Internal locks (lock.c) ----

// Spin-then-futex lock for runtime internal state. While held, the M is in a
// runtime critical section (m->locks > 0) and cannot be stopped by a signal.
typedef struct rt_spin { _Atomic uint32_t v; } rt_spin;
void spin_lock(rt_spin *l);
void spin_unlock(rt_spin *l);

// One-shot sleep/wakeup for an M. notesleep blocks the OS thread.
typedef struct note { _Atomic uint32_t key; } note;
void noteclear(note *n);
void notewakeup(note *n);
void notesleep(note *n);
// Returns true if woken, false on timeout.
bool notetsleep(note *n, int64_t ns);

// ---- Arch (arch_*.S) ----

// Save callee-saved registers on the current stack, store the stack pointer
// in *save, switch to stack pointer `to`, restore and return there.
void rt_ctx_switch(void **save, void *to);
// Build the initial frame of a new context on a stack whose top is `top`, so
// that switching to the returned sp calls fn(arg). fn must not return.
void *rt_ctx_init(void *top, void (*fn)(void *), void *arg);
// Spill callee-saved registers into buf (at least 24 words) and return the
// current stack pointer. Used before conservative scans of a stack.
void *rt_spill_regs(void *buf);

// ---- Goroutines and scheduler (sched.c) ----

enum {
  G_IDLE = 0,   // on a free list
  G_RUNNABLE,
  G_RUNNING,
  G_WAITING,
  G_SYSCALL,
  G_DEAD,
};

struct rt_g {
  void *sp;               // saved stack pointer while not running
  uintptr_t stack_lo;     // lowest usable address (above the guard page)
  uintptr_t stack_hi;     // top of stack
  uintptr_t map_base;     // start of the mapping (guard page)
  uintptr_t map_size;
  _Atomic uint32_t status;
  bool system;            // runtime goroutine, not counted, not in deadlock check
  bool preempt;           // yield at the next scheduling point
  int64_t id;
  void (*fn)(void *);
  void *arg;              // GC root until the goroutine starts
  void *slot;             // rt_gslot, GC root
  void *param;            // rt_gparam, GC root
  rt_m *m;                // M running this G, or NULL
  rt_g *schedlink;        // run queue and free list link
  uint32_t gidx;          // index in the goroutine table
  void *tsan_fiber;       // ThreadSanitizer builds only
  void *syscallsp;        // sp saved at rt_entersyscall
  void *syscallregs[24];  // callee-saved registers at rt_entersyscall
  rt_timer *sleeptimer;   // rt_sleep, allocated on first use (not GC memory)
};

// Request from a goroutine to its M's scheduler after switching to g0.
enum {
  REQ_NONE = 0,
  REQ_PARK,
  REQ_GOSCHED,
  REQ_EXIT,
  REQ_EXITSYSCALL,  // exitsyscall found no P: queue the goroutine globally
  REQ_HANDOFF,      // park and run m->handoff_g next on this M
  REQ_YIELD,        // requeue at the tail of this P's run queue
};

enum {
  P_IDLE = 0,
  P_RUNNING,
  P_SYSCALL,
  P_GCSTOP,
  P_DEAD,      // beyond GOMAXPROCS after a shrink
};

#define RUNQ_SIZE 256

struct rt_p {
  int32_t id;
  _Atomic uint32_t status;
  rt_m *m;                       // owner while running
  rt_p *link;                    // idle list
  _Atomic uint32_t runqhead;
  _Atomic uint32_t runqtail;
  rt_g *_Atomic runnext;
  rt_g *_Atomic runq[RUNQ_SIZE];  // relaxed atomics: stealers read slots racily
  mcache *mcache;
  _Atomic uint32_t syscalltick;  // incremented on each syscall entry
  uint32_t sysmon_tick;          // last tick seen by sysmon
  int64_t sysmon_when;
  _Atomic uint32_t schedtick;
  uint32_t sysmon_schedtick;
  int64_t sysmon_schedwhen;
  _Atomic int64_t ngo;           // goroutines started minus exited on this P (relaxed, owner writes)
  int64_t goidnext, goidend;     // cached block of goroutine ids
  rt_g *gfree;                   // local free goroutines (no stacks)
  int32_t ngfree;
  uintptr_t stackcache[32];      // free stacks: mapping base addresses
  int32_t nstackcache;
};

struct rt_m {
  rt_g g0;                  // scheduler context on the thread stack
  rt_g *curg;               // running goroutine or NULL
  rt_p *p;                  // attached P
  mcache *mcache;           // p->mcache, cached for the allocation fast path
  void *rcache;             // region.c per-thread caches
  rt_p *nextp;              // P handed over before wakeup
  rt_m *alllink;
  rt_m *idlelink;
  int64_t id;
  volatile int32_t locks;   // >0: runtime critical section, no async stop (only this thread and its signal handler touch it)
  bool spinning;            // looking for work
  bool idle;                // on the idle M list
  note park;
  // Scheduling request from curg.
  int32_t req;
  rt_g *handoff_g;          // REQ_HANDOFF target, then the next goroutine to run
  void (*unlockf)(void *);
  void *unlockarg;
  // Stop-the-world state.
  _Atomic int32_t stop_ack;  // set when stopped for GC, with stop_sp valid
  void *stop_sp;             // stack pointer at the stop point
  uintptr_t stop_hi;         // top of the stack stop_sp is on
  void *thread;              // pthread_t
  uint64_t fastrand;
  uintptr_t sigstack;        // alternate signal stack
  // MARMOT_DEADLOCK_DEBUG: SIGURG deliveries, stops, and why the last one
  // did not stop (sigurg's checks, in order: 1 no gcwaiting, 2 locks, 3 no
  // curg, 4 no P, 5 P not running, 6 P owned by another M, 7 not on the
  // goroutine stack).
  _Atomic uint32_t nsigurg, nsigstop;
  _Atomic int32_t sigurg_why;
};

// Current M. The thread pointer is read with volatile asm on every call, so
// the compiler never reuses a value across a goroutine switch that moved the
// goroutine to another thread (which a plain _Thread_local access allows).
// darwin: a pthread TSD slot (rt_m_tlsoff = key * 8); linux: offset of
// rt_tls_m from the thread pointer.
extern uintptr_t rt_m_tlsoff;
extern _Thread_local rt_m *rt_tls_m;
INLINE rt_m *getm(void) {
#if RT_OS_DARWIN && RT_ARCH_ARM64
  uintptr_t tp;
  __asm__ volatile("mrs %0, tpidrro_el0" : "=r"(tp));
  return *(rt_m **)((tp & ~(uintptr_t)7) + rt_m_tlsoff);
#elif RT_OS_DARWIN && RT_ARCH_AMD64
  rt_m *m;
  __asm__ volatile("movq %%gs:(%1), %0" : "=r"(m) : "r"(rt_m_tlsoff));
  return m;
#elif RT_OS_LINUX && RT_ARCH_ARM64
  uintptr_t tp;
  __asm__ volatile("mrs %0, tpidr_el0" : "=r"(tp));
  return *(rt_m **)(tp + rt_m_tlsoff);
#elif RT_OS_LINUX && RT_ARCH_AMD64
  uintptr_t tp;
  __asm__ volatile("movq %%fs:0, %0" : "=r"(tp));
  return *(rt_m **)(tp + rt_m_tlsoff);
#endif
}
// Set the current thread's M. tls_init must run first (os_init does it).
void setm(rt_m *m);
void tls_init(void);

typedef struct schedt {
  rt_spin lock;
  rt_g *runqhead, *runqtail;  // global run queue
  _Atomic int32_t runqsize;   // written under lock, read without it
  rt_p *pidle;
  _Atomic int32_t npidle;
  rt_m *midle;
  int32_t nmidle;
  _Atomic int32_t nmspinning;
  int32_t mcount;             // Ms created
  int32_t nmsys;              // Ms not counted for deadlock (sysmon)
  rt_m *allm;
  rt_p *allp[256];           // MAXPROCS
  _Atomic int32_t nallp;      // allocated Ps
  _Atomic int32_t nprocs;     // GOMAXPROCS
  rt_m *_Atomic timer_m;      // idle M sleeping until the next timer
  int64_t timer_until;
  rt_g *gfree;                // global free goroutines
  int32_t ngfree;
  // Stop the world.
  _Atomic int32_t gcwaiting;
  int32_t stopwait;
  note stopnote;
  _Atomic uint32_t worldgen;  // bumped by start-the-world; Ms wait on it
  // Goroutine accounting.
  rt_spin glock;              // protects the goroutine table (sched.c)
  _Atomic int64_t ngoroutines; // user goroutines alive
  _Atomic int64_t goidgen;
  bool main_exited;           // main goroutine called rt_goexit
} schedt;
extern schedt sched;

// MARMOT_DEADLOCK_DEBUG (watchdog.c): print every M, P and G (sched.c).
void sched_dump(void);
void watchdog_start(void);
uint32_t gc_mu_state(void);
extern rt_g *rt_main_g;

void sched_init(void);
void sched_start_main(void) NORETURN;
// Stop and restart the world. Called on a goroutine stack, with no
// runtime locks held. The caller keeps its P.
void stop_the_world(void);
void start_the_world(void);
// Safepoint for STW: call where it is safe to stop (no runtime locks).
void rt_safepoint(void);
// Called by an M that sees gcwaiting set (from a safepoint or signal).
void sched_stop_self(void *sp, uintptr_t hi);
void wakep(void);
// Like rt_go for runtime goroutines: not counted, ignored by deadlock checks.
// Iterate goroutines and Ms for the GC. World must be stopped.
void sched_scan_roots(void (*scan)(uintptr_t lo, uintptr_t hi));
rt_g *new_system_g(void (*fn)(void *), void *arg);
// signal.c
void signal_init(void);
void signal_init_thread(rt_m *m);
void signal_preempt(rt_m *m);
void signal_raise_sigpipe(void);
// Ready the goroutine in rt_signal_recv if signals are pending (sysmon).
bool signal_deliver(void);
// A goroutine is parked in rt_signal_recv.
bool signal_waiting(void);
void signal_check_stuck(void);
void sysmon_start(void);
void sysmon_kick(void);
// netpoll.c
bool netpoll_inited(void);
int32_t netpoll(int64_t ns);  // ns < 0 blocks; returns events handled
void netpoll_break(void);
extern _Atomic int32_t netpoll_waiters;
extern _Atomic int32_t netpoll_blocked;
extern _Atomic int64_t netpoll_last;

// Optional subsystems. The scheduler and the GC reach timers, the poller and
// sysmon only through these hooks, which the subsystems install on first
// use (a timer added, a descriptor opened, a goroutine started), so
// programs that never use them do not link them (dead stripping).
extern int64_t (*hook_timers_check)(int64_t now);
extern void (*hook_timers_scan)(void (*scan)(uintptr_t lo, uintptr_t hi));
extern int32_t (*hook_netpoll)(int64_t ns);
extern void (*hook_netpoll_break)(void);
extern void (*hook_sysmon_start)(void);
void gc_parallel_enable(void);
void sched_multi(void);  // first goroutine beyond main: sysmon and parallel GC hooks

// ---- Timers (timer.c) ----

// Run due timers. Returns the next deadline or -1. Called on g0 with a P.
int64_t timers_check(int64_t now);
int64_t timers_next(void);
void timers_scan_roots(void (*scan)(uintptr_t lo, uintptr_t hi));
extern _Atomic int64_t timer_next_when;  // INT64_MAX when no timers
// Internal timers (not GC memory). fn runs inside timers_check on whichever
// M checks timers: it must be short and only take spin locks or rt_ready.
rt_timer *itimer_new(void);
void itimer_start(rt_timer *t, int64_t when, void (*fn)(void *), void *arg);
bool itimer_stop(rt_timer *t);  // true if stopped before firing
// Called by timer code after adding a timer that fires at when.
void sched_timer_added(int64_t when);

// ---- Regions (region.c) ----
void regions_scan_roots(void (*scan)(uintptr_t lo, uintptr_t hi));
void regions_trim(void);

// ---- Memory (malloc.c, gc.c) ----

void malloc_init(void);
void mcache_flush_all(void);
mcache *mcache_new(void);
void mcache_free(mcache *c);
void gc_init(void);
void gc_scan_range(uintptr_t lo, uintptr_t hi);
// Called by the allocator slow path when the heap reaches the trigger.
void gc_maybe_start(void);
extern int64_t rt_gcstress;  // collect every N allocations, 0 off

#pragma GCC visibility pop

#endif
