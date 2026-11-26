// race.c: the race detector's runtime side (marmot build -race). Built only
// into librtcore_race.a (RT_RACE=1); a normal build compiles this file to
// nothing, so its output does not change.
//
// Compiled Go code is instrumented by LLVM's ThreadSanitizer pass, which calls
// the C interface of TSan (__tsan_read4(addr), __tsan_func_entry(pc), the
// __tsan_atomic* calls). The TSan runtime we link is its Go flavor
// (compiler-rt/lib/tsan/go, as gc links), which wants a goroutine context and a
// pc with every event. This file maps the one onto the other:
//   - the context is the running goroutine's (g->tsan_fiber holds it in race
//     builds; the runtime's own TSan test build uses that field differently and
//     is never a race build);
//   - only heap arena and data/bss addresses reach TSan, as gc's racecalladdr:
//     goroutine stacks, libc memory and other mappings are skipped;
//   - atomics build gc's argument block for __tsan_go_atomic*.
// Four names exist in both interfaces with different arguments (__tsan_init,
// __tsan_read_range, __tsan_write_range, __tsan_func_exit): the build step
// renames them in the Go flavor object to __gotsan_*.
#include "internal.h"
#include "malloc.h"

#if RT_RACE

typedef unsigned long uptr;

// The Go flavor of TSan (renamed where the names collide).
void __gotsan_init(void **thr, void **proc, void (*cb)(uptr cmd, void *ctx));
void __gotsan_read_range(void *thr, void *addr, uptr size, uptr pc);
void __gotsan_write_range(void *thr, void *addr, uptr size, uptr pc);
void __tsan_fini(void);
void __tsan_map_shadow(void *addr, uptr size);
void __tsan_read(void *thr, void *addr, void *pc);
void __tsan_write(void *thr, void *addr, void *pc);
void __tsan_func_enter(void *thr, void *pc);
void __gotsan_func_exit(void *thr);
void __tsan_malloc(void *thr, uptr pc, uptr p, uptr sz);
void __tsan_free(uptr p, uptr sz);
void __tsan_go_start(void *thr, void **chthr, void *pc);
void __tsan_go_end(void *thr);
void __tsan_proc_create(void **pproc);
void __tsan_proc_destroy(void *proc);
void __tsan_acquire(void *thr, void *addr);
void __tsan_release(void *thr, void *addr);
void __tsan_release_merge(void *thr, void *addr);
void __tsan_release_acquire(void *thr, void *addr);
void __tsan_finalizer_goroutine(void *thr);
void __tsan_report_count(unsigned long long *n);
void __tsan_go_ignore_sync_begin(void *thr);
void __tsan_go_ignore_sync_end(void *thr);
void __tsan_go_atomic32_load(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic64_load(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic32_store(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic64_store(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic32_fetch_add(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic64_fetch_add(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic32_fetch_and(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic64_fetch_and(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic32_fetch_or(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic64_fetch_or(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic32_exchange(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic64_exchange(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic32_compare_exchange(void *thr, uptr cpc, uptr pc, void *a);
void __tsan_go_atomic64_compare_exchange(void *thr, uptr cpc, uptr pc, void *a);

static void *race_main_ctx;  // the initial context from __gotsan_init
static void *race_proc0;
static uintptr_t data_lo, data_hi;
bool rt_race_on;

INLINE void *racectx(void) {
  rt_m *m = getm();
  rt_g *g = m ? m->curg : NULL;
  return g && g->tsan_fiber ? g->tsan_fiber : race_main_ctx;
}

// Nonzero while this thread runs the TSan callback or runtime code whose
// annotated operations are not the program's (rt_race_skip). TSan holds its
// locks in the callback, so an access the runtime annotates inside it (the
// symbolizer's copy into sym_fn) must not enter TSan again: a race on the
// static buffers started a second report that waited forever for the first
// one's locks.
static __thread int race_skip;
RT_API void rt_race_skip(int32_t d) { race_skip += d; }

// gc's isvalidaddr: the heap arena and the program's data and bss.
INLINE bool race_addr(const void *p) {
  if (race_skip) return false;
  uintptr_t a = (uintptr_t)p;
  return (a >= rt_arena_start && a < rt_arena_end) || (a >= data_lo && a < data_hi);
}

// ---- TSan callback: Processor and symbolization ----

struct code_ctx { uptr pc; const char *func, *file; uptr line, off, res; };
struct data_ctx { uptr addr, heap, start, size; const char *name, *file; uptr line, res; };

// Go side (lib/src/runtime/race_marmot.go): function, file and line of pc from
// the pc table, copied into these buffers (TSan copies them at once; reports
// run one at a time). Weak: a program without it prints "runtime".
__attribute__((weak)) bool runtime_race_symbolize(uintptr_t pc, char *fn, intptr_t fncap, char *file, intptr_t filecap,
                                                  intptr_t *line);
static char sym_fn[512], sym_file[1024];
// The P's TSan Processor (see race_proc).
// The calls that can report run as a runtime critical section (NOSTOP below)
// unless MARMOT_RACE_NOSTOP=0.
static bool race_nostop = true;
static __thread void *thread_proc;

static void race_callback(uptr cmd, void *ctx) {
  race_skip++;
  switch (cmd) {
  case 0:  // the current Processor: one per OS thread for now (TODO: per P)
    if (!thread_proc) __tsan_proc_create(&thread_proc);
    *(void **)ctx = thread_proc;
    break;
  case 1: {  // code
    struct code_ctx *c = ctx;
    intptr_t line = 0;
    // Answer for every pc: TSan skips an unanswered frame and then drops the
    // outermost one it has (gc's goexit frame), which would be a user frame.
    bool ok = runtime_race_symbolize &&
              runtime_race_symbolize(c->pc, sym_fn, sizeof sym_fn, sym_file, sizeof sym_file, &line);
    c->func = ok ? sym_fn : "runtime";
    c->file = ok ? sym_file : "?";
    c->line = (uptr)line;
    c->off = 0;
    c->res = 1;
    break;
  }
  case 2: {  // data: heap objects by their span
    struct data_ctx *d = ctx;
    void *base = NULL;
    uintptr_t size = 0;
    if (rt_heap_object_span((void *)d->addr, &base, &size) != RT_HEAPOBJ_NONE) {
      d->heap = 1;
      d->start = (uptr)base;
      d->size = size;
      d->res = 1;
    }
    break;
  }
  }
  race_skip--;
}

// ---- start, goroutines, exit ----

void race_init(void) {
  const char *ns = rt_getenv("MARMOT_RACE_NOSTOP");
  if (ns && ns[0] == '0') race_nostop = false;
  extern char __data_start[], _end[];
  data_lo = (uintptr_t)__data_start;
  data_hi = (uintptr_t)_end;
  __gotsan_init(&race_main_ctx, &race_proc0, race_callback);
  thread_proc = race_proc0;
  __tsan_map_shadow((void *)data_lo, data_hi - data_lo);
  __tsan_map_shadow((void *)rt_arena_start, rt_arena_end - rt_arena_start);
  rt_race_on = true;
}

// The parent goroutine (or the initial context for the main goroutine) starts
// child g at the go statement pc.
void race_go_start(rt_g *child, void *pc) {
  if (!rt_race_on) return;
  __tsan_go_start(racectx(), &child->tsan_fiber, pc);
}

void race_go_end(rt_g *g) {
  if (!rt_race_on || !g->tsan_fiber) return;
  __tsan_go_end(g->tsan_fiber);
  g->tsan_fiber = NULL;
}

void race_malloc(void *p, uintptr_t size, void *pc) {
  if (rt_race_on) __tsan_malloc(racectx(), (uptr)pc, (uptr)p, size);
}

void race_free(void *p, uintptr_t size) {
  if (rt_race_on) __tsan_free((uptr)p, size);
}

// Exit status 66 when races were found (GORACE exitcode is read by TSan for
// its own fini; gc exits 66 by default).
int race_fini(void) {
  if (!rt_race_on) return 0;
  __tsan_fini();
  unsigned long long n = 0;
  __tsan_report_count(&n);
  return n ? 66 : 0;
}

// Synchronization for the Go side (runtime.RaceAcquire and friends, channels).
RT_API void rt_race_acquire(void *addr) { if (rt_race_on) __tsan_acquire(racectx(), addr); }
RT_API void rt_race_release(void *addr) { if (rt_race_on) __tsan_release(racectx(), addr); }
RT_API void rt_race_release_merge(void *addr) { if (rt_race_on) __tsan_release_merge(racectx(), addr); }
RT_API void rt_race_release_acquire(void *addr) { if (rt_race_on) __tsan_release_acquire(racectx(), addr); }
// A runtime object that starts a new life without an allocation (a reused
// goroutine's g): its old accesses are forgotten, as for a new heap object.
RT_API void rt_race_reset(void *p, uintptr_t n) {
  if (!rt_race_on) return;
  __tsan_free((uptr)p, n);
  __tsan_malloc(racectx(), (uptr)__builtin_return_address(0), (uptr)p, n);
}
RT_API void rt_race_fingo(void) { if (rt_race_on) __tsan_finalizer_goroutine(racectx()); }
// pc 0: the caller of the Go function that called this (internal/race.Read
// and friends pass no pc of their own). Frame records are kept in every
// frame (-fno-omit-frame-pointer, Go code non-leaf), so level 1 is safe here.
#pragma clang diagnostic ignored "-Wframe-address"
// A TSan call that can print a report holds TSan's report lock while it
// calls back into the runtime (the symbolizer, Go code). An async stop or a
// safepoint must not park this thread there, or a thread that stops the
// world and one that waits for the report lock wait for each other. So the
// call runs as a runtime critical section (m->locks), as gc's racecall runs
// on g0.
// MARMOT_RACE_NOSTOP=0 turns this off (race_nostop: see race_init).
#define NOSTOP(call)                                                           \
  do {                                                                         \
    rt_m *m_ = race_nostop ? getm() : NULL;                                    \
    if (m_) m_->locks++;                                                       \
    call;                                                                      \
    if (m_) m_->locks--;                                                       \
  } while (0)
#define GOPC(pc) ((pc) ? (pc) : (uintptr_t)__builtin_return_address(1))
RT_API NOINLINE void rt_race_read(void *addr, uintptr_t pc) {
  if (race_addr(addr)) NOSTOP(__tsan_read(racectx(), addr, (void *)GOPC(pc)));
}
RT_API NOINLINE void rt_race_write(void *addr, uintptr_t pc) {
  if (race_addr(addr)) NOSTOP(__tsan_write(racectx(), addr, (void *)GOPC(pc)));
}
RT_API NOINLINE void rt_race_read_range(void *addr, uintptr_t n, uintptr_t pc) {
  if (n && race_addr(addr)) NOSTOP(__gotsan_read_range(racectx(), addr, n, GOPC(pc)));
}
RT_API NOINLINE void rt_race_write_range(void *addr, uintptr_t n, uintptr_t pc) {
  if (n && race_addr(addr)) NOSTOP(__gotsan_write_range(racectx(), addr, n, GOPC(pc)));
}
// internal/race.Disable and Enable: ignore synchronization in between (gc).
RT_API void rt_race_ignore(int32_t begin) {
  if (!rt_race_on) return;
  if (begin) __tsan_go_ignore_sync_begin(racectx());
  else __tsan_go_ignore_sync_end(racectx());
}
RT_API intptr_t rt_race_errors(void) {
  unsigned long long n = 0;
  if (rt_race_on) __tsan_report_count(&n);
  return (intptr_t)n;
}

// ---- the C interface the TSan pass calls ----

#define PC() ((void *)__builtin_return_address(0))
#define ACCESS(name, kind)                                                      \
  RT_API void __tsan_##name(void *addr) {                                       \
    if (race_addr(addr)) NOSTOP(__tsan_##kind(racectx(), addr, PC()));          \
  }
ACCESS(read1, read) ACCESS(read2, read) ACCESS(read4, read) ACCESS(read8, read) ACCESS(read16, read)
ACCESS(write1, write) ACCESS(write2, write) ACCESS(write4, write) ACCESS(write8, write) ACCESS(write16, write)
ACCESS(unaligned_read2, read) ACCESS(unaligned_read4, read) ACCESS(unaligned_read8, read) ACCESS(unaligned_read16, read)
ACCESS(unaligned_write2, write) ACCESS(unaligned_write4, write) ACCESS(unaligned_write8, write) ACCESS(unaligned_write16, write)
// Volatile accesses (the pass's option, off by default) count as plain ones.
ACCESS(volatile_read1, read) ACCESS(volatile_read2, read) ACCESS(volatile_read4, read) ACCESS(volatile_read8, read) ACCESS(volatile_read16, read)
ACCESS(volatile_write1, write) ACCESS(volatile_write2, write) ACCESS(volatile_write4, write) ACCESS(volatile_write8, write) ACCESS(volatile_write16, write)

RT_API void __tsan_read_range(void *addr, uptr size) {
  if (size && race_addr(addr)) NOSTOP(__gotsan_read_range(racectx(), addr, size, (uptr)PC()));
}
RT_API void __tsan_write_range(void *addr, uptr size) {
  if (size && race_addr(addr)) NOSTOP(__gotsan_write_range(racectx(), addr, size, (uptr)PC()));
}

RT_API void __tsan_func_entry(void *pc) { __tsan_func_enter(racectx(), pc); }
RT_API void __tsan_func_exit(void) { __gotsan_func_exit(racectx()); }

// The pass replaces memcpy, memmove and memset with these.
RT_API void *__tsan_memcpy(void *dst, const void *src, uptr n) {
  if (n && race_addr(src)) NOSTOP(__gotsan_read_range(racectx(), (void *)src, n, (uptr)PC()));
  if (n && race_addr(dst)) NOSTOP(__gotsan_write_range(racectx(), dst, n, (uptr)PC()));
  return __builtin_memcpy(dst, src, n);
}
RT_API void *__tsan_memmove(void *dst, const void *src, uptr n) {
  if (n && race_addr(src)) NOSTOP(__gotsan_read_range(racectx(), (void *)src, n, (uptr)PC()));
  if (n && race_addr(dst)) NOSTOP(__gotsan_write_range(racectx(), dst, n, (uptr)PC()));
  return __builtin_memmove(dst, src, n);
}
RT_API void *__tsan_memset(void *dst, int c, uptr n) {
  if (n && race_addr(dst)) NOSTOP(__gotsan_write_range(racectx(), dst, n, (uptr)PC()));
  return __builtin_memset(dst, c, n);
}

// ---- atomics: gc's argument blocks ----
// Layout, as tsan_interface_atomic.cpp reads it: the address at 0, the
// operand at 8; a load's result at 8; a read-modify-write's result at 16 for
// both sizes; compare-and-swap: old at 8, new at 8 + size, the bool result at
// 8 + 2 * size. A C struct would put a 32-bit result at 12, so the blocks are
// byte arrays with explicit offsets.
// Memory orders do not matter to the Go flavor: Go atomics are sequentially
// consistent, and TSan treats them as acquire and release.

#define BLK uint8_t b[32] __attribute__((aligned(8)))
#define AT(T, off) (*(T *)(b + (off)))

#define GOATOMIC_LOAD(bits, T)                                                   \
  RT_API T __tsan_atomic##bits##_load(const volatile T *a, int mo) {             \
    (void)mo;                                                                    \
    if (!race_addr((void *)a)) return __atomic_load_n(a, __ATOMIC_SEQ_CST);      \
    BLK;                                                                         \
    AT(const volatile T *, 0) = a;                                               \
    NOSTOP(__tsan_go_atomic##bits##_load(racectx(), (uptr)PC(), (uptr)PC(), b)); \
    return AT(T, 8);                                                             \
  }
#define GOATOMIC_STORE(bits, T)                                                  \
  RT_API void __tsan_atomic##bits##_store(volatile T *a, T v, int mo) {          \
    (void)mo;                                                                    \
    if (!race_addr((void *)a)) { __atomic_store_n(a, v, __ATOMIC_SEQ_CST); return; } \
    BLK;                                                                         \
    AT(volatile T *, 0) = a;                                                     \
    AT(T, 8) = v;                                                                \
    NOSTOP(__tsan_go_atomic##bits##_store(racectx(), (uptr)PC(), (uptr)PC(), b)); \
  }
#define GOATOMIC_RMW(bits, T, op, builtin)                                       \
  RT_API T __tsan_atomic##bits##_##op(volatile T *a, T v, int mo) {              \
    (void)mo;                                                                    \
    if (!race_addr((void *)a)) return builtin(a, v, __ATOMIC_SEQ_CST);           \
    BLK;                                                                         \
    AT(volatile T *, 0) = a;                                                     \
    AT(T, 8) = v;                                                                \
    NOSTOP(__tsan_go_atomic##bits##_##op(racectx(), (uptr)PC(), (uptr)PC(), b)); \
    return AT(T, 16);                                                            \
  }
#define GOATOMIC_CAS(bits, T)                                                    \
  RT_API int __tsan_atomic##bits##_compare_exchange_strong(volatile T *a, T *c, T v, int mo, int fmo) { \
    (void)mo, (void)fmo;                                                         \
    if (!race_addr((void *)a)) return __atomic_compare_exchange_n(a, c, v, false, __ATOMIC_SEQ_CST, __ATOMIC_SEQ_CST); \
    BLK;                                                                         \
    AT(volatile T *, 0) = a;                                                     \
    AT(T, 8) = *c;                                                               \
    AT(T, 8 + sizeof(T)) = v;                                                    \
    NOSTOP(__tsan_go_atomic##bits##_compare_exchange(racectx(), (uptr)PC(), (uptr)PC(), b)); \
    bool ok = AT(bool, 8 + 2 * sizeof(T));                                       \
    if (!ok) *c = __atomic_load_n(a, __ATOMIC_SEQ_CST);                          \
    return ok;                                                                   \
  }                                                                              \
  RT_API int __tsan_atomic##bits##_compare_exchange_weak(volatile T *a, T *c, T v, int mo, int fmo) { \
    return __tsan_atomic##bits##_compare_exchange_strong(a, c, v, mo, fmo);      \
  }                                                                              \
  RT_API T __tsan_atomic##bits##_compare_exchange_val(volatile T *a, T c, T v, int mo, int fmo) { \
    T cur = c;                                                                   \
    __tsan_atomic##bits##_compare_exchange_strong(a, &cur, v, mo, fmo);          \
    return cur;                                                                  \
  }

GOATOMIC_LOAD(32, uint32_t) GOATOMIC_LOAD(64, uint64_t)
GOATOMIC_STORE(32, uint32_t) GOATOMIC_STORE(64, uint64_t)
GOATOMIC_RMW(32, uint32_t, fetch_add, __atomic_fetch_add) GOATOMIC_RMW(64, uint64_t, fetch_add, __atomic_fetch_add)
GOATOMIC_RMW(32, uint32_t, fetch_and, __atomic_fetch_and) GOATOMIC_RMW(64, uint64_t, fetch_and, __atomic_fetch_and)
GOATOMIC_RMW(32, uint32_t, fetch_or, __atomic_fetch_or) GOATOMIC_RMW(64, uint64_t, fetch_or, __atomic_fetch_or)
GOATOMIC_RMW(32, uint32_t, exchange, __atomic_exchange_n) GOATOMIC_RMW(64, uint64_t, exchange, __atomic_exchange_n)
GOATOMIC_CAS(32, uint32_t) GOATOMIC_CAS(64, uint64_t)

// fetch_sub: an add of the negation. 8 and 16 bit atomics (no Go operations
// use them; LLVM may emit them for bool flags): the operation itself, then a
// release and acquire on the address so it still orders.
RT_API uint32_t __tsan_atomic32_fetch_sub(volatile uint32_t *a, uint32_t v, int mo) { return __tsan_atomic32_fetch_add(a, -v, mo); }
RT_API uint64_t __tsan_atomic64_fetch_sub(volatile uint64_t *a, uint64_t v, int mo) { return __tsan_atomic64_fetch_add(a, -v, mo); }
#define SMALLATOMIC(bits, T)                                                     \
  RT_API T __tsan_atomic##bits##_load(const volatile T *a, int mo) {             \
    (void)mo; T v = __atomic_load_n(a, __ATOMIC_SEQ_CST);                        \
    if (race_addr((void *)a)) __tsan_acquire(racectx(), (void *)a);              \
    return v;                                                                    \
  }                                                                              \
  RT_API void __tsan_atomic##bits##_store(volatile T *a, T v, int mo) {          \
    (void)mo; if (race_addr((void *)a)) __tsan_release(racectx(), (void *)a);    \
    __atomic_store_n(a, v, __ATOMIC_SEQ_CST);                                    \
  }                                                                              \
  RT_API T __tsan_atomic##bits##_exchange(volatile T *a, T v, int mo) {          \
    (void)mo; if (race_addr((void *)a)) __tsan_release_acquire(racectx(), (void *)a); \
    return __atomic_exchange_n(a, v, __ATOMIC_SEQ_CST);                          \
  }                                                                              \
  RT_API int __tsan_atomic##bits##_compare_exchange_strong(volatile T *a, T *c, T v, int mo, int fmo) { \
    (void)mo, (void)fmo; if (race_addr((void *)a)) __tsan_release_acquire(racectx(), (void *)a); \
    return __atomic_compare_exchange_n(a, c, v, false, __ATOMIC_SEQ_CST, __ATOMIC_SEQ_CST); \
  }
SMALLATOMIC(8, uint8_t) SMALLATOMIC(16, uint16_t)

// The rest of what LLVM's pass can emit. 32 and 64 bit xor and nand have no
// Go flavor entry: a compare-and-swap loop through the Go flavor, so they
// order as gc's atomics do. 8 and 16 bit: as SMALLATOMIC.
#define CASLOOP(bits, T, op, expr)                                                \
  RT_API T __tsan_atomic##bits##_fetch_##op(volatile T *a, T v, int mo) {         \
    T old = __atomic_load_n(a, __ATOMIC_RELAXED);                                 \
    while (!__tsan_atomic##bits##_compare_exchange_strong(a, &old, (T)(expr), mo, mo)) { \
    }                                                                             \
    return old;                                                                   \
  }
CASLOOP(32, uint32_t, xor, old ^ v) CASLOOP(64, uint64_t, xor, old ^ v)
CASLOOP(32, uint32_t, nand, ~(old & v)) CASLOOP(64, uint64_t, nand, ~(old & v))
#define SMALLRMW(bits, T, op, builtin)                                            \
  RT_API T __tsan_atomic##bits##_fetch_##op(volatile T *a, T v, int mo) {         \
    (void)mo; if (race_addr((void *)a)) __tsan_release_acquire(racectx(), (void *)a); \
    return builtin(a, v, __ATOMIC_SEQ_CST);                                       \
  }
#define SMALLREST(bits, T)                                                        \
  SMALLRMW(bits, T, add, __atomic_fetch_add)                                      \
  SMALLRMW(bits, T, sub, __atomic_fetch_sub)                                      \
  SMALLRMW(bits, T, and, __atomic_fetch_and)                                      \
  SMALLRMW(bits, T, or, __atomic_fetch_or)                                        \
  SMALLRMW(bits, T, xor, __atomic_fetch_xor)                                      \
  SMALLRMW(bits, T, nand, __atomic_fetch_nand)                                    \
  RT_API int __tsan_atomic##bits##_compare_exchange_weak(volatile T *a, T *c, T v, int mo, int fmo) { \
    return __tsan_atomic##bits##_compare_exchange_strong(a, c, v, mo, fmo);       \
  }                                                                               \
  RT_API T __tsan_atomic##bits##_compare_exchange_val(volatile T *a, T c, T v, int mo, int fmo) { \
    T cur = c;                                                                    \
    __tsan_atomic##bits##_compare_exchange_strong(a, &cur, v, mo, fmo);           \
    return cur;                                                                   \
  }
SMALLREST(8, uint8_t) SMALLREST(16, uint16_t)

RT_API void __tsan_atomic_thread_fence(int mo) { (void)mo; __atomic_thread_fence(__ATOMIC_SEQ_CST); }
RT_API void __tsan_atomic_signal_fence(int mo) { (void)mo; }

#endif  // RT_RACE
