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
// Tell the OS the pages may be discarded; later reads see zero or old data.
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
  rt_g *alllink;          // all goroutines list
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
  rt_g *runq[RUNQ_SIZE];
  mcache *mcache;
  _Atomic uint32_t syscalltick;  // incremented on each syscall entry
  uint32_t sysmon_tick;          // last tick seen by sysmon
  int64_t sysmon_when;
  _Atomic uint32_t schedtick;
  uint32_t sysmon_schedtick;
  int64_t sysmon_schedwhen;
  rt_g *gfree;                   // local free goroutines
  int32_t ngfree;
};

struct rt_m {
  rt_g g0;                  // scheduler context on the thread stack
  rt_g *curg;               // running goroutine or NULL
  rt_p *p;                  // attached P
  rt_p *nextp;              // P handed over before wakeup
  rt_m *alllink;
  rt_m *idlelink;
  int64_t id;
  _Atomic int32_t locks;    // >0: runtime critical section, no async stop
  bool spinning;            // looking for work
  bool idle;                // on the idle M list
  note park;
  // Scheduling request from curg.
  int32_t req;
  void (*unlockf)(void *);
  void *unlockarg;
  // Stop-the-world state.
  _Atomic int32_t stop_ack;  // set when stopped for GC, with stop_sp valid
  void *stop_sp;             // stack pointer at the stop point
  uintptr_t stop_hi;         // top of the stack stop_sp is on
  void *thread;              // pthread_t
  uint64_t fastrand;
  uintptr_t sigstack;        // alternate signal stack
};

extern _Thread_local rt_m *rt_tls_m;
// Current M. noinline so that the TLS address is recomputed after a switch.
rt_m *getm(void) NOINLINE;

typedef struct schedt {
  rt_spin lock;
  rt_g *runqhead, *runqtail;  // global run queue
  int32_t runqsize;
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
  rt_m *timer_m;              // idle M sleeping until the next timer
  int64_t timer_until;
  rt_g *gfree;                // global free goroutines
  int32_t ngfree;
  // Stop the world.
  _Atomic int32_t gcwaiting;
  int32_t stopwait;
  note stopnote;
  _Atomic uint32_t worldgen;  // bumped by start-the-world; Ms wait on it
  // Goroutine accounting.
  rt_spin glock;              // protects allg
  rt_g *allg;
  _Atomic int64_t ngoroutines; // user goroutines alive
  _Atomic int64_t goidgen;
  bool main_exited;           // main goroutine called rt_goexit
} schedt;
extern schedt sched;
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
void sched_release_free_stacks(void);
// signal.c
void signal_init(void);
void signal_init_thread(rt_m *m);
void signal_preempt(rt_m *m);
void signal_raise_sigpipe(void);

// ---- Timers (timer.c) ----

void timer_init(void);
// Run due timers. Returns the next deadline or -1. Called on g0 with a P.
int64_t timers_check(int64_t now);
int64_t timers_next(void);
void timers_scan_roots(void (*scan)(uintptr_t lo, uintptr_t hi));
extern _Atomic int64_t timer_next_when;  // INT64_MAX when no timers
// Called by timer code after adding a timer that fires at when.
void sched_timer_added(int64_t when);

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

#endif
