// gc.c: stop-the-world, non-moving mark-sweep collector.
//
// Roots: writable data segments of the executable (conservative), ranges
// from rt_gc_add_roots, goroutine stacks from their stop point up
// (conservative, registers spilled), per-goroutine slot, param and start
// argument, pending timers and queued finalizers. Heap objects are scanned
// by kind: noscan objects are not scanned, conservative ones word by word,
// precise ones through rt_heapbits (small) or their span's ptrmap (large).
// Any pointer into an object marks it.
//
// Finalizers: after marking from roots, each unmarked object with a
// finalizer has its referents marked (not itself), then those still
// unmarked are marked, queued, and lose their finalizer. They are freed by
// the next cycle if still unreachable.

#include "malloc.h"

static rt_mutex gc_mu;
uint32_t gc_mu_state(void) { return __atomic_load_n((uint32_t *)&gc_mu.state, __ATOMIC_RELAXED); }
static int gc_trace;  // MARMOT_GCTRACE: 1 a line per cycle, 2 also the heap by size class
static _Atomic uint64_t num_gc;
static uint64_t pause_total_ns;

// ---- write barrier (concurrent marking, not on yet) ----

RT_API volatile uint8_t rt_gc_marking;

// Shades both the value overwritten and the one stored (the hybrid
// barrier): neither can hide from a mark that runs beside the program.
// Not reached while marking stops the world (rt_gc_marking stays 0).
RT_API void rt_wb(void **slot, void *new_value) {
  (void)slot;
  (void)new_value;
}

RT_API void rt_wb_range(void *dst, const void *src, uintptr_t n) {
  (void)dst;
  (void)src;
  (void)n;
}

// ---- registered roots ----

typedef struct {
  uintptr_t lo, hi;
} range;
static range *roots;
static int32_t nroots, caproots;
static rt_spin roots_lock;

RT_API void rt_gc_add_roots(void *start, void *end) {
  spin_lock(&roots_lock);
  if (nroots == caproots) {
    int32_t ncap = caproots ? caproots * 2 : 64;
    range *n = persistent_alloc((uintptr_t)ncap * sizeof *n, 8);
    if (nroots) __builtin_memcpy(n, roots, (uintptr_t)nroots * sizeof *n);
    roots = n;
    caproots = ncap;
  }
  roots[nroots].lo = (uintptr_t)start;
  roots[nroots].hi = (uintptr_t)end;
  nroots++;
  spin_unlock(&roots_lock);
}

// ---- finalizers ----

typedef struct {
  uintptr_t obj;
  void (*fn)(void *);
} finrec;

static rt_spin fin_lock;
static finrec *fintab;      // open addressing, obj 0 = empty, 1 = deleted
static uintptr_t fincap, finlen, finused;
static finrec *finq;        // queued to run, GC roots
static uintptr_t finqlen, finqcap;
static rt_g *fing;
static bool fing_parked;

static uintptr_t fin_hash(uintptr_t obj, uintptr_t cap) {
  return (uintptr_t)((obj >> 3) * 0x9E3779B97F4A7C15ull) & (cap - 1);
}

static void fintab_insert(uintptr_t obj, void (*fn)(void *));

static void fintab_grow(void) {
  finrec *old = fintab;
  uintptr_t oldcap = fincap;
  fincap = fincap ? fincap * 2 : 64;
  fintab = os_map(fincap * sizeof(finrec));
  if (!fintab) rt_throw("out of memory (finalizers)");
  finlen = finused = 0;
  for (uintptr_t i = 0; i < oldcap; i++)
    if (old[i].obj > 1) fintab_insert(old[i].obj, old[i].fn);
  if (old) os_unmap(old, oldcap * sizeof(finrec));
}

static void fintab_insert(uintptr_t obj, void (*fn)(void *)) {
  if ((finused + 1) * 4 >= fincap * 3) fintab_grow();
  uintptr_t i = fin_hash(obj, fincap);
  intptr_t tomb = -1;
  for (;; i = (i + 1) & (fincap - 1)) {
    if (fintab[i].obj == obj) {
      fintab[i].fn = fn;
      return;
    }
    if (fintab[i].obj == 1 && tomb < 0) tomb = (intptr_t)i;
    if (fintab[i].obj == 0) break;
  }
  if (tomb >= 0) {
    i = (uintptr_t)tomb;
  } else {
    finused++;
  }
  fintab[i].obj = obj;
  fintab[i].fn = fn;
  finlen++;
}

static void fintab_remove(uintptr_t obj) {
  if (!fincap) return;
  for (uintptr_t i = fin_hash(obj, fincap);; i = (i + 1) & (fincap - 1)) {
    if (fintab[i].obj == 0) return;
    if (fintab[i].obj == obj) {
      fintab[i].obj = 1;
      fintab[i].fn = NULL;
      finlen--;
      return;
    }
  }
}

static void finq_push(uintptr_t obj, void (*fn)(void *)) {
  if (finqlen == finqcap) {
    uintptr_t ncap = finqcap ? finqcap * 2 : 64;
    finrec *n = os_map(ncap * sizeof *n);
    if (!n) rt_throw("out of memory (finalizer queue)");
    if (finqlen) __builtin_memcpy(n, finq, finqlen * sizeof *n);
    if (finq) os_unmap(finq, finqcap * sizeof *n);
    finq = n;
    finqcap = ncap;
  }
  finq[finqlen].obj = obj;
  finq[finqlen].fn = fn;
  finqlen++;
}

static void fin_unlock(void *unused) { spin_unlock(&fin_lock); }

static void finproc(void *unused) {
  for (;;) {
    spin_lock(&fin_lock);
    while (finqlen == 0) {
      fing_parked = true;
      rt_park(fin_unlock, NULL);
      spin_lock(&fin_lock);
    }
    finrec r = finq[--finqlen];
    finq[finqlen].obj = 0;
    spin_unlock(&fin_lock);
    r.fn((void *)r.obj);
    r.obj = 0;
  }
}

bool gc_has_finalizers(void) { return finlen > 0; }

// ---- the idle collection ----
//
// A program that goes idle after allocating keeps its heap until it
// allocates again, which an idle program does not. Once every P has been
// idle for a while (sysmon decides), a system goroutine collects once and
// returns every free span to the OS: one wakeup after a burst, none while
// the program stays idle (nothing allocated since that collection).

static _Atomic uint64_t gc_alloc_mark;  // stat_total_alloc at the last collection
static rt_g *idle_gc_g;
static _Atomic uint32_t idle_gc_parked;  // 1 while idle_gc_g waits to be readied

// Bytes allocated since the last collection, past which an idle program
// is worth one collection.
#define IDLE_GC_MIN (1u << 20)

static void idle_gc_park_done(void *arg) { atomic_store(&idle_gc_parked, 1); }

static void idle_gc_proc(void *arg) {
  for (;;) {
    rt_park(idle_gc_park_done, NULL);
    gc_collect(true);
    spin_lock(&heap_lock);
    heap_scavenge_min(0, rt_pagesize);
    spin_unlock(&heap_lock);
  }
}

bool gc_idle_wanted(void) {
  return gc_percent >= 0 && atomic_load(&stat_total_alloc) - atomic_load(&gc_alloc_mark) >= IDLE_GC_MIN;
}

// From sysmon: start the idle collection (no P needed).
void gc_idle_start(void) {
  uint32_t one = 1;
  if (idle_gc_g && atomic_compare_exchange_strong(&idle_gc_parked, &one, 0)) rt_ready(idle_gc_g);
}

// Made at startup, before the main goroutine (its stack is reserved only
// when it first runs).
void gc_idle_init(void) { idle_gc_g = new_system_g(idle_gc_proc, NULL); }

RT_API void rt_gc_set_finalizer(void *obj, void (*fn)(void *)) {
  uintptr_t p = (uintptr_t)obj;
  if (p - rt_arena_start >= rt_arena_used - rt_arena_start) return;  // not a heap object
  if (fn && !fing) {
    rt_g *g = new_system_g(finproc, NULL);
    spin_lock(&fin_lock);
    if (!fing)
      fing = g;
    else
      rt_throw("rt_gc_set_finalizer: finalizer goroutine created twice");
    spin_unlock(&fin_lock);
  }
  spin_lock(&fin_lock);
  if (fn)
    fintab_insert(p, fn);
  else
    fintab_remove(p);
  spin_unlock(&fin_lock);
}

// ---- marking ----
//
// Parallel mark: the collecting thread scans the roots into its own work
// stack, then it and up to gc_nhelpers helper threads drain the heap graph.
// A worker with a deep stack shares half of it through the global list of
// full work buffers when another worker is idle. Mark bits are set with an
// atomic or. Marking ends when every worker is idle and no buffer is left.

#define WBUF_N 1022
typedef struct wbuf {
  struct wbuf *next;
  uintptr_t n;
  uintptr_t obj[WBUF_N];
} wbuf;

// Each worker writes its gcw on every object: one cache line each (128
// bytes on Apple silicon) so workers do not falsely share.
typedef struct gcw {
  uintptr_t *stk;
  uintptr_t len, cap;
  uint64_t nscan;  // objects scanned this cycle (trace)
  uint64_t marked_bytes;  // bytes of objects this worker marked (MARMOT_GCTRACE=3)
} __attribute__((aligned(128))) gcw;

#define MAX_GCWORKERS 16
static gcw workers[MAX_GCWORKERS];
static rt_spin wlock;
static wbuf *_Atomic wfull;
static wbuf *wempty;
static _Atomic int32_t wnidle;
// Workers that joined this mark phase. Only grows during a phase: the
// collector starts alone and helpers join if they get a CPU while the phase
// is open, so a busy machine never makes the collector wait for them.
static _Atomic int32_t wactive;
static int32_t gc_nhelpers = -1;     // helper threads (not counting the collector)
static uint64_t last_live;           // heap_live after the previous collection
static _Atomic uint32_t helper_gen;  // bumped to start a phase
static _Atomic int32_t phase_open;   // helpers may join
static _Atomic int32_t phase_joined; // helpers inside the current phase

static void gcw_grow(gcw *w) {
  uintptr_t ncap = w->cap ? w->cap * 2 : 65536;
  uintptr_t *n = os_map(ncap * sizeof *n);
  if (!n) rt_throw("out of memory (mark stack)");
  if (w->len) __builtin_memcpy(n, w->stk, w->len * sizeof *n);
  if (w->stk) os_unmap(w->stk, w->cap * sizeof *n);
  w->stk = n;
  w->cap = ncap;
}

INLINE void gcw_push(gcw *w, uintptr_t obj) {
  if (UNLIKELY(w->len == w->cap)) gcw_grow(w);
  w->stk[w->len++] = obj;
}

static wbuf *wbuf_get_empty(void) {
  spin_lock(&wlock);
  wbuf *b = wempty;
  if (b) wempty = b->next;
  spin_unlock(&wlock);
  if (!b) {
    b = os_map(sizeof(wbuf));
    if (!b) rt_throw("out of memory (mark buffers)");
  }
  b->n = 0;
  b->next = NULL;
  return b;
}

static _Atomic uint32_t wwork_gen;  // bumped when work is shared or marking ends
static _Atomic int32_t wdone;
static _Atomic int32_t wsleeping;   // idle workers blocked in the kernel
static uint32_t gc_spin = 4000;    // idle spins before sleeping (MARMOT_GCSPIN)

static void wbuf_put_full(wbuf *b) {
  spin_lock(&wlock);
  b->next = atomic_load(&wfull);
  atomic_store(&wfull, b);
  spin_unlock(&wlock);
  atomic_fetch_add(&wwork_gen, 1);
  if (atomic_load(&wsleeping) > 0) os_futex_wake(&wwork_gen, false);
}

static wbuf *wbuf_get_full(void) {
  if (!atomic_load(&wfull)) return NULL;
  spin_lock(&wlock);
  wbuf *b = atomic_load(&wfull);
  if (b) atomic_store(&wfull, b->next);
  spin_unlock(&wlock);
  return b;
}

static void wbuf_put_empty(wbuf *b) {
  spin_lock(&wlock);
  b->next = wempty;
  wempty = b;
  spin_unlock(&wlock);
}

// Find the object containing p. Returns its span and index, or NULL.
NO_SANITIZE INLINE mspan *find_object(uintptr_t p, uintptr_t *idxp) {
  if (p - rt_arena_start >= rt_arena_used - rt_arena_start) return NULL;
  mspan *s = rt_pagemap[(p - rt_arena_start) >> PAGE_SHIFT];
  if (!s || s->state != SPAN_INUSE || p < s->base || p >= s->limit) return NULL;
  uintptr_t idx = s->cls ? (uintptr_t)(((uint64_t)(p - s->base) * s->divmul) >> 32) : 0;
  if (idx >= s->nelems) return NULL;
  if (!((s->allocbits[idx / 64] >> (idx % 64)) & 1)) return NULL;  // free slot
  *idxp = idx;
  return s;
}

NO_SANITIZE INLINE void mark_ptr(gcw *w, uintptr_t p) {
  uintptr_t idx;
  mspan *s = find_object(p, &idx);
  if (!s) return;
  uint64_t bit = 1ull << (idx % 64);
  uint64_t *mw = &s->markbits[idx / 64];
  if (__atomic_load_n(mw, __ATOMIC_RELAXED) & bit) return;
  if (__atomic_fetch_or(mw, bit, __ATOMIC_RELAXED) & bit) return;
  w->marked_bytes += s->elemsize;
  if (s->kind != RT_ALLOC_NOSCAN) gcw_push(w, s->base + idx * s->elemsize);
}

NO_SANITIZE static void scan_range_w(gcw *w, uintptr_t lo, uintptr_t hi) {
  lo = (lo + WORDSZ - 1) & ~(WORDSZ - 1);
  for (uintptr_t p = lo; p + WORDSZ <= hi; p += WORDSZ) mark_ptr(w, *(uintptr_t *)p);
}

// MARMOT_GCTRACE=3: the roots that keep the most bytes alive. Each root
// word that marks a new object is drained at once (serially) and charged
// what that marked.
typedef struct {
  uintptr_t addr;   // the root word
  uint64_t bytes;
  int64_t goid;     // 0: not a goroutine stack
  uintptr_t pc;     // the frame owning the word (a stack root), 0: unknown
  uintptr_t off;    // from the stack's top
} root_rec;
static root_rec top_roots[20];
static rt_g *root_g;  // the goroutine whose stack is being scanned
static void drain_serial(gcw *w);

void gc_root_goroutine(rt_g *g) { root_g = g; }

// The frame owning stack word a of g: the function whose frame pointer is
// the first above a; its pc is the return address saved in the record
// below it (the callee's), found from the saved context's frame chain.
static NOINLINE uintptr_t owner_pc(rt_g *g, uintptr_t a) {
  if (!g) return 0;
  rt_m *me = getm();
  uintptr_t fp, pc;
  if (me && me->curg == g) {
    // The collecting goroutine: its frames are below, from here.
    fp = (uintptr_t)__builtin_frame_address(0);
    pc = 0;
    goto walk;
  }
  if (!g->sp || atomic_load(&g->status) == G_RUNNING) return 0;
#if RT_ARCH_ARM64
  fp = ((uintptr_t *)g->sp)[10], pc = ((uintptr_t *)g->sp)[11];
#elif RT_ARCH_AMD64
  fp = ((uintptr_t *)g->sp)[5], pc = ((uintptr_t *)g->sp)[6];
#else
  fp = 0, pc = 0;
#endif
walk:
  for (int i = 0; i < 256 && fp >= g->stack_lo && fp + 16 <= g->stack_hi; i++) {
    if (a < fp) return pc;
    pc = ((uintptr_t *)fp)[1];
    uintptr_t up = ((uintptr_t *)fp)[0];
    if (up <= fp) break;
    fp = up;
  }
  return pc;
}

NO_SANITIZE static void scan_range_roots(gcw *w, uintptr_t lo, uintptr_t hi) {
  lo = (lo + WORDSZ - 1) & ~(WORDSZ - 1);
  for (uintptr_t p = lo; p + WORDSZ <= hi; p += WORDSZ) {
    uint64_t before = w->marked_bytes;
    mark_ptr(w, *(uintptr_t *)p);
    if (w->marked_bytes == before) continue;
    drain_serial(w);
    uint64_t got = w->marked_bytes - before;
    int k = 0;
    for (int i = 1; i < 20; i++)
      if (top_roots[i].bytes < top_roots[k].bytes) k = i;
    if (got > top_roots[k].bytes) {
      root_rec r = {p, got, root_g ? root_g->id : 0, 0, 0};
      if (root_g) {
        r.pc = owner_pc(root_g, p);
        r.off = root_g->stack_hi - p;
      }
      top_roots[k] = r;
    }
  }
}

static void print_roots(void) {
  rt_print("  top roots (bytes marked first from each):\n");
  for (int n = 0; n < 20; n++) {
    int k = -1;
    for (int i = 0; i < 20; i++)
      if (top_roots[i].bytes && (k < 0 || top_roots[i].bytes > top_roots[k].bytes)) k = i;
    if (k < 0) break;
    root_rec r = top_roots[k];
    top_roots[k].bytes = 0;
    rt_print("    ");
    rt_printu(r.bytes >> 10);
    rt_print(" KiB from ");
    rt_printx(r.addr);
    if (r.goid) {
      rt_print(" goroutine ");
      rt_printi(r.goid);
      rt_print(" stack top-");
      rt_printu(r.off);
      rt_print(" frame pc ");
      rt_printx(r.pc);
    } else {
      rt_print(" (data segment or registered root)");
    }
    rt_print("\n");
  }
}

// Root scanning runs on the collecting thread, into worker 0.
NO_SANITIZE void gc_scan_range(uintptr_t lo, uintptr_t hi) {
  if (UNLIKELY(gc_trace >= 3)) {
    scan_range_roots(&workers[0], lo, hi);
    return;
  }
  scan_range_w(&workers[0], lo, hi);
}

NO_SANITIZE static void scan_object(gcw *w, uintptr_t obj) {
  mspan *s = rt_pagemap[(obj - rt_arena_start) >> PAGE_SHIFT];
  if (s->kind == RT_ALLOC_CONSERVATIVE) {
    scan_range_w(w, obj, obj + s->elemsize);
  } else if (s->cls) {
    // Small precise object: heap bits.
    uintptr_t w0 = (obj - rt_arena_start) / WORDSZ;
    uintptr_t nw = s->elemsize / WORDSZ;
    const uintptr_t *o = (const uintptr_t *)obj;
    for (uintptr_t i = 0; i < nw;) {
      uintptr_t wd = w0 + i;
      uint64_t bits = rt_heapbits[wd / 64] >> (wd % 64);
      uintptr_t n = 64 - wd % 64;
      if (n > nw - i) {
        n = nw - i;
        bits &= (1ull << n) - 1;
      }
      while (bits) {
        uintptr_t b = (uintptr_t)__builtin_ctzll(bits);
        bits &= bits - 1;
        mark_ptr(w, o[i + b]);
      }
      i += n;
    }
  } else {
    // Large precise object: repeat the element ptrmap.
    uintptr_t ew = s->ptrelem / WORDSZ;
    const uint8_t *pm = s->ptrmap;
    for (uintptr_t e = 0; e < s->ptrcount; e++) {
      const uintptr_t *elem = (const uintptr_t *)(obj + e * s->ptrelem);
      for (uintptr_t i = 0; i < ew; i++)
        if ((pm[i / 8] >> (i % 8)) & 1) mark_ptr(w, elem[i]);
    }
  }
}

// Move the oldest half (at most one buffer) of w's stack to the shared list.
static void share(gcw *w) {
  uintptr_t n = w->len / 2;
  if (n > WBUF_N) n = WBUF_N;
  wbuf *b = wbuf_get_empty();
  __builtin_memcpy(b->obj, w->stk, n * sizeof(uintptr_t));
  b->n = n;
  __builtin_memmove(w->stk, w->stk + n, (w->len - n) * sizeof(uintptr_t));
  w->len -= n;
  wbuf_put_full(b);
}

INLINE void cpu_relax(void) {
#if RT_ARCH_ARM64
  __asm__ volatile("yield");
#elif RT_ARCH_AMD64
  __asm__ volatile("pause");
#endif  // wasm: one thread, nothing to wait for
}

// Drain until global termination. Single-worker phases just drain.
static void drain_parallel(gcw *w) {
  for (;;) {
    uint32_t k = 0;
    // Objects popped from the stack pass through a small FIFO and are
    // prefetched on entry, so their cache misses overlap.
    uintptr_t pf[8];
    uint32_t ph = 0, pn = 0;
    while (w->len > 0 || pn > 0) {
      if (w->len > 0 && pn < 8) {
        uintptr_t o = w->stk[--w->len];
        __builtin_prefetch((const void *)o);
        pf[(ph + pn) & 7] = o;
        pn++;
        continue;
      }
      uintptr_t o = pf[ph];
      ph = (ph + 1) & 7;
      pn--;
      scan_object(w, o);
      w->nscan++;
      // Share half the stack when someone is idle and nothing is queued.
      if ((++k & 31) == 0 && w->len > 4 && atomic_load_explicit(&wnidle, memory_order_relaxed) > 0 &&
          !atomic_load_explicit(&wfull, memory_order_relaxed))
        share(w);
    }
    wbuf *b = wbuf_get_full();
    if (b) {
      while (w->cap < w->len + b->n) gcw_grow(w);
      __builtin_memcpy(w->stk + w->len, b->obj, b->n * sizeof(uintptr_t));
      w->len += b->n;
      wbuf_put_empty(b);
      continue;
    }
    if (atomic_load(&wactive) == 1 && !atomic_load(&wfull)) return;  // alone: nobody holds work
    if (atomic_load(&wdone)) return;
    if (atomic_fetch_add(&wnidle, 1) + 1 == atomic_load(&wactive) && !atomic_load(&wfull)) {
      // Last worker to go idle with nothing queued: marking is done.
      atomic_store(&wdone, 1);
      atomic_fetch_add(&wwork_gen, 1);
      os_futex_wake(&wwork_gen, true);
      return;
    }
    for (uint32_t spins = 0;; spins++) {
      if (atomic_load(&wdone)) return;
      if (atomic_load(&wfull)) {
        atomic_fetch_sub(&wnidle, 1);
        break;
      }
      if (atomic_load(&wnidle) == atomic_load(&wactive) && !atomic_load(&wfull)) {
        atomic_store(&wdone, 1);
        atomic_fetch_add(&wwork_gen, 1);
        os_futex_wake(&wwork_gen, true);
        return;
      }
      if (spins < gc_spin) {
        cpu_relax();
      } else {
        uint32_t g = atomic_load(&wwork_gen);
        atomic_fetch_add(&wsleeping, 1);
        if (!atomic_load(&wfull) && !atomic_load(&wdone)) os_futex_wait(&wwork_gen, g, 200000);
        atomic_fetch_sub(&wsleeping, 1);
        spins = gc_spin / 2;
      }
    }
  }
}

// One worker, no helpers: the stack loop of drain_parallel without sharing
// or termination detection.
static void drain_serial(gcw *w) {
  uintptr_t pf[8];
  uint32_t ph = 0, pn = 0;
  while (w->len > 0 || pn > 0) {
    if (w->len > 0 && pn < 8) {
      uintptr_t o = w->stk[--w->len];
      __builtin_prefetch((const void *)o);
      pf[(ph + pn) & 7] = o;
      pn++;
      continue;
    }
    uintptr_t o = pf[ph];
    ph = (ph + 1) & 7;
    pn--;
    scan_object(w, o);
    w->nscan++;
  }
}

// Parallel marking and sweeping (helper threads, work sharing) are linked
// only into programs that call gc_parallel_enable: rt_go does (a program
// with one goroutine collects serially), and so does
// rt_gc_parallel_enable, which a driver can force with -u. Until then the
// collector drains alone and gc_nhelpers stays 0.
static void (*drain_fn)(gcw *) = drain_serial;
static void (*helpers_init_fn)(void);

// Single-threaded drain (finalizer marking); no phase is open.
static void drain(void) {
  atomic_store(&wactive, 1);
  atomic_store(&wdone, 0);
  drain_fn(&workers[0]);
}

enum { PHASE_MARK = 0, PHASE_SWEEP = 1 };
static _Atomic int32_t gc_phase;
static void sweep_worker(void);

static void gc_helper(void *arg) {
  gcw *w = arg;
  uint32_t seen = 0;
  for (;;) {
    uint32_t g;
    while ((g = atomic_load(&helper_gen)) == seen) os_futex_wait(&helper_gen, seen, -1);
    if (trace_on()) trace_wake_record(&(trace_why){.cause = WAKE_GC});
    seen = g;
    atomic_fetch_add(&phase_joined, 1);
    if (atomic_load(&phase_open)) {
      if (atomic_load(&gc_phase) == PHASE_SWEEP) {
        sweep_worker();
      } else {
        atomic_fetch_add(&wactive, 1);
        if (!atomic_load(&wdone)) drain_parallel(w);
      }
    }
    atomic_fetch_sub(&phase_joined, 1);
  }
}

static void helpers_init(void) {
  static bool started;  // gc_nhelpers may be 0 from serial collections before
  if (started) return;
  started = true;
  bool ok;
  int64_t n = rt_atoi(rt_getenv("MARMOT_GCPROCS"), &ok);
  if (!ok || n < 1) {
    // Half the processors, at most 4 workers: most of the speedup for a
    // modest CPU cost on a shared machine.
    n = atomic_load(&sched.nprocs);
    int32_t c = os_ncpu();
    if (n > c) n = c;
    n = n / 2;
    if (n < 1) n = 1;
    if (n > 4) n = 4;
  }
  if (n > MAX_GCWORKERS) n = MAX_GCWORKERS;
  int64_t sp = rt_atoi(rt_getenv("MARMOT_GCSPIN"), &ok);
  if (ok && sp >= 0) gc_spin = (uint32_t)sp;
  gc_nhelpers = 0;
  for (int32_t i = 1; i < n; i++) {
    if (!os_thread_create(gc_helper, &workers[i], 256 << 10)) break;
    gc_nhelpers++;
  }
}

// Open a phase and wake the helpers; they join if they arrive in time.
static void helpers_start(int32_t phase, int32_t nh) {
  atomic_store(&gc_phase, phase);
  atomic_store(&phase_open, 1);
  atomic_fetch_add(&helper_gen, 1);
  os_futex_wake(&helper_gen, true);
}

// Close the phase: helpers that joined finish quickly (the work is done);
// helpers that arrive later see it closed and go back to sleep.
static void helpers_wait(void) {
  atomic_store(&phase_open, 0);
  while (atomic_load(&phase_joined) > 0) cpu_relax();
}

// Drain the heap graph from worker 0's stack with all helpers.
static void mark_parallel(void) {
  int32_t nh = gc_nhelpers;
  // Small heaps are not worth waking the helpers for. Marking work follows
  // the live data, so the size that survived the last collection decides,
  // not heap_live (which counts the garbage of this cycle too).
  if (last_live < (2u << 20) || workers[0].len == 0) nh = 0;
  atomic_store(&wactive, 1);
  atomic_store(&wnidle, 0);
  atomic_store(&wdone, 0);
  if (nh > 0) helpers_start(PHASE_MARK, nh);
  drain_fn(&workers[0]);
  // The collector may return without wdone when it finished alone; helpers
  // that joined after that must see marking as done, or they wait for work
  // forever while helpers_wait waits for them.
  atomic_store(&wdone, 1);
  if (nh > 0) {
    atomic_fetch_add(&wwork_gen, 1);
    os_futex_wake(&wwork_gen, true);
    helpers_wait();
  }
}

static bool is_marked(uintptr_t p) {
  uintptr_t idx;
  mspan *s = find_object(p, &idx);
  return !s || ((s->markbits[idx / 64] >> (idx % 64)) & 1);
}

static void set_marked(uintptr_t p) {
  uintptr_t idx;
  mspan *s = find_object(p, &idx);
  if (s) s->markbits[idx / 64] |= 1ull << (idx % 64);
}

// ---- weak pointers ----
//
// Registered slots hold object addresses that do not keep the object alive.
// After marking and before finalizers mark their referents (gc order: a weak
// pointer to an object that is only alive for its finalizer reads nil), every
// slot whose object is unmarked is zeroed and dropped. A slot in heap memory
// that is itself unreachable is dropped without a write. Open addressing on
// the slot address; deleted entries are tombstones (1).

static rt_spin weak_lock;
static uintptr_t *weak_tab;
static uintptr_t weak_cap, weak_used, weak_live;

static uintptr_t weak_hash(uintptr_t a) { return (uintptr_t)rt_hash_u64(a, 0x77656b); }

static void weak_insert_raw(uintptr_t slot) {
  uintptr_t i = weak_hash(slot) & (weak_cap - 1);
  while (weak_tab[i] > 1) i = (i + 1) & (weak_cap - 1);
  if (weak_tab[i] == 0) weak_used++;
  weak_tab[i] = slot;
  weak_live++;
}

static void weak_grow(void) {
  uintptr_t ocap = weak_cap, *otab = weak_tab;
  weak_cap = weak_live * 4 > 1024 ? weak_live * 4 : 1024;
  uintptr_t c = 1;
  while (c < weak_cap) c <<= 1;
  weak_cap = c;
  weak_tab = os_map(weak_cap * sizeof *weak_tab);
  if (!weak_tab) rt_throw("out of memory (weak pointer table)");
  weak_used = weak_live = 0;
  for (uintptr_t i = 0; i < ocap; i++)
    if (otab[i] > 1) weak_insert_raw(otab[i]);
  if (otab) os_unmap(otab, ocap * sizeof *otab);
}

RT_API void rt_gc_weak_register(uintptr_t *slot) {
  spin_lock(&weak_lock);
  if ((weak_used + 1) * 2 > weak_cap) weak_grow();
  uintptr_t a = (uintptr_t)slot, i = weak_hash(a) & (weak_cap - 1);
  for (;;) {
    if (weak_tab[i] == a) break;  // already registered
    if (weak_tab[i] == 0) {
      weak_insert_raw(a);
      break;
    }
    i = (i + 1) & (weak_cap - 1);
  }
  spin_unlock(&weak_lock);
}

RT_API void rt_gc_weak_unregister(uintptr_t *slot) {
  spin_lock(&weak_lock);
  if (weak_cap) {
    uintptr_t a = (uintptr_t)slot, i = weak_hash(a) & (weak_cap - 1);
    while (weak_tab[i] != 0) {
      if (weak_tab[i] == a) {
        weak_tab[i] = 1;
        weak_live--;
        break;
      }
      i = (i + 1) & (weak_cap - 1);
    }
  }
  spin_unlock(&weak_lock);
}

// The collector stops the world, so a load outside a collection is safe as
// is: the value then sits in a register or on the stack, which the next
// collection scans.
RT_API void *rt_gc_weak_load(uintptr_t *slot) {
  return (void *)__atomic_load_n(slot, __ATOMIC_ACQUIRE);
}

static bool is_marked(uintptr_t p);

// World stopped, marking done, finalizer referents not yet marked.
static void weak_clear(void) {
  for (uintptr_t i = 0; i < weak_cap; i++) {
    uintptr_t a = weak_tab[i];
    if (a <= 1) continue;
    if (!is_marked(a)) {  // the slot's own memory is garbage
      weak_tab[i] = 1;
      weak_live--;
      continue;
    }
    uintptr_t obj = *(uintptr_t *)a;
    if (obj && !is_marked(obj)) {
      *(uintptr_t *)a = 0;
      weak_tab[i] = 1;
      weak_live--;
    }
  }
}

static void scan_data_segment(void *lo, void *hi) { gc_scan_range((uintptr_t)lo, (uintptr_t)hi); }

static void mark_roots(void) {
  gcw *w = &workers[0];
  os_data_segments(scan_data_segment);
  for (int32_t i = 0; i < nroots; i++) gc_scan_range(roots[i].lo, roots[i].hi);
  sched_scan_roots(gc_scan_range);
  void (*ts)(void (*)(uintptr_t, uintptr_t)) = __atomic_load_n(&hook_timers_scan, __ATOMIC_ACQUIRE);
  if (ts) ts(gc_scan_range);
  regions_scan_roots(gc_scan_range);
  for (uintptr_t i = 0; i < finqlen; i++) mark_ptr(w, finq[i].obj);
  if (UNLIKELY(rt_poison)) poison_gc(set_marked);
  mark_parallel();
}

static bool mark_finalizers(void) {
  bool queued = false;
  if (!finlen) return false;
  gcw *w = &workers[0];
  // Referents of finalizable objects stay alive.
  for (uintptr_t i = 0; i < fincap; i++) {
    uintptr_t obj = fintab[i].obj;
    if (obj <= 1 || is_marked(obj)) continue;
    uintptr_t idx;
    mspan *s = find_object(obj, &idx);
    if (s && s->kind != RT_ALLOC_NOSCAN) {
      scan_object(w, s->base + idx * s->elemsize);
      drain();
    }
  }
  // Objects still unmarked are unreachable: queue their finalizers.
  for (uintptr_t i = 0; i < fincap; i++) {
    uintptr_t obj = fintab[i].obj;
    if (obj <= 1 || is_marked(obj)) continue;
    finq_push(obj, fintab[i].fn);
    set_marked(obj);
    fintab[i].obj = 1;
    fintab[i].fn = NULL;
    finlen--;
    queued = true;
  }
  return queued;
}

// ---- sweeping ----

// Sweeping runs in two passes. The parallel pass turns mark bits into
// allocation bits span by span (spans are independent). The serial pass
// frees empty spans and rebuilds the central lists, which is cheap.
#define SWEEP_BATCH 128
static _Atomic int32_t sweep_next;
static _Atomic uint64_t sweep_freed_bytes, sweep_freed_objs;

static void sweep_span(mspan *s, uint64_t *fb, uint64_t *fo) {
  uint32_t nw = (s->nelems + 63) / 64;
  uintptr_t freed = 0, live = 0;
  for (uint32_t w = 0; w < nw; w++) {
    freed += popcount64(s->allocbits[w] & ~s->markbits[w]);
    live += popcount64(s->markbits[w]);
  }
  *fb += freed * s->elemsize;
  *fo += freed;
  uint64_t *t = s->allocbits;
  s->allocbits = s->markbits;
  s->markbits = t;
  __builtin_memset(t, 0, nw * 8);
  s->freeindex = 0;
  s->alloc_cache = 0;
  s->swept_live = (uint32_t)live;
}

static void sweep_worker(void) {
  uint64_t fb = 0, fo = 0;
  for (;;) {
    int32_t i = atomic_fetch_add(&sweep_next, SWEEP_BATCH);
    if (i >= nallspans) break;
    int32_t end = i + SWEEP_BATCH < nallspans ? i + SWEEP_BATCH : nallspans;
    for (int32_t j = i; j < end; j++) sweep_span(allspans[j], &fb, &fo);
  }
  atomic_fetch_add(&sweep_freed_bytes, fb);
  atomic_fetch_add(&sweep_freed_objs, fo);
}

static void sweep_all(void) {
  spin_lock(&heap_lock);
  central_reset();
  atomic_store(&sweep_next, 0);
  atomic_store(&sweep_freed_bytes, 0);
  atomic_store(&sweep_freed_objs, 0);
  int32_t nh = nallspans >= 4 * SWEEP_BATCH ? gc_nhelpers : 0;
  if (nh > 0) helpers_start(PHASE_SWEEP, nh);
  sweep_worker();
  if (nh > 0) helpers_wait();
  for (int32_t i = nallspans - 1; i >= 0; i--) {
    mspan *s = allspans[i];
    if (s->swept_live == 0) {
      span_free(s);
    } else if (s->cls) {
      s->needzero = 1;
      central_put(s);
    }
  }
  atomic_fetch_sub(&heap_live, atomic_load(&sweep_freed_bytes));
  atomic_fetch_add(&stat_frees, atomic_load(&sweep_freed_objs));
  heap_scavenge(next_gc == UINT64_MAX ? atomic_load(&heap_live) : next_gc);
  spin_unlock(&heap_lock);
}

// ---- collection ----

// MARMOT_GCTRACE=2: span bytes against live bytes by size class (after the
// sweep), the classes that waste the most first, and large objects.
NOINLINE static void gc_print_classes(void) {
  uint64_t span[NUM_CLASSES] = {0}, used[NUM_CLASSES] = {0}, nspans[NUM_CLASSES] = {0};
  spin_lock(&heap_lock);
  for (int32_t i = 0; i < nallspans; i++) {
    mspan *s = allspans[i];
    if (s->cls >= NUM_CLASSES) continue;
    span[s->cls] += s->npages * PAGE_SIZE;
    used[s->cls] += (uint64_t)s->swept_live * s->elemsize;
    nspans[s->cls]++;
  }
  spin_unlock(&heap_lock);
  uint64_t ts = 0, tu = 0;
  for (int c = 0; c < NUM_CLASSES; c++) ts += span[c], tu += used[c];
  rt_print("  heap by class: spans ");
  rt_printu(ts >> 10);
  rt_print(" KiB, live ");
  rt_printu(tu >> 10);
  rt_print(" KiB; large objects ");
  rt_printu(span[0] >> 10);
  rt_print(" KiB in ");
  rt_printu(nspans[0]);
  rt_print(" spans\n");
  bool shown[NUM_CLASSES] = {false};
  for (int k = 0; k < 10; k++) {
    int best = -1;
    for (int c = 1; c < NUM_CLASSES; c++)
      if (!shown[c] && (best < 0 || span[c] - used[c] > span[best] - used[best])) best = c;
    if (best < 0 || span[best] == used[best]) break;
    shown[best] = true;
    rt_print("    class ");
    rt_printu((uint64_t)best);
    rt_print(" (");
    rt_printu(rt_class_size[best]);
    rt_print(" B): ");
    rt_printu(nspans[best]);
    rt_print(" spans, ");
    rt_printu(span[best] >> 10);
    rt_print(" KiB, live ");
    rt_printu(used[best] >> 10);
    rt_print(" KiB\n");
  }
}

NOINLINE static void gc_print_trace(uint64_t before, uint64_t live, int64_t t0, int64_t t1,
                                  int64_t t2, int64_t t3, int64_t t4) {
  rt_print("gc ");
  rt_printu(atomic_load(&num_gc));
  rt_print(": ");
  rt_printu(before >> 10);
  rt_print(" -> ");
  rt_printu(live >> 10);
  rt_print(" KiB, goal ");
  rt_printu(next_gc >> 10);
  rt_print(" KiB, stw ");
  rt_printu((uint64_t)(t1 - t0) / 1000);
  rt_print(" us, mark ");
  rt_printu((uint64_t)(t2 - t1) / 1000);
  rt_print(" us, sweep ");
  rt_printu((uint64_t)(t3 - t2) / 1000);
  rt_print(" us, total ");
  rt_printu((uint64_t)(t4 - t0) / 1000);
  rt_print(" us, ");
  rt_printu((uint64_t)gc_nhelpers + 1);
  rt_print(" workers, scanned");
  for (int32_t i = 0; i <= gc_nhelpers; i++) {
    rt_print(" ");
    rt_printu(workers[i].nscan);
    workers[i].nscan = 0;
  }
  rt_print(", heap ");
  rt_printu(heap_sys >> 10);
  rt_print(" KiB, released ");
  rt_printu(heap_released >> 10);
  rt_print(" KiB\n");
  }

void gc_collect(bool forced) {
#if !RT_OS_WASI
  // sysmon starts the idle collection; a program that never starts a
  // second thread has no sysmon otherwise.
  sysmon_start();
#endif
  uint64_t gen = atomic_load(&num_gc);
  rt_lock(&gc_mu);
  if (!forced && (atomic_load(&num_gc) != gen || atomic_load(&heap_live) < next_gc)) {
    rt_unlock(&gc_mu);
    return;
  }
  // Helper threads are created before the world stops: pthread_create
  // takes libc locks (malloc) that a thread stopped by signal may hold.
  void (*hinit)(void) = __atomic_load_n(&helpers_init_fn, __ATOMIC_ACQUIRE);
  if (hinit) {
    hinit();
  } else if (gc_nhelpers < 0) {
    gc_nhelpers = 0;
  }
  int64_t t0 = os_nanotime();
  stop_the_world();
  rt_m *m = getm();
  void *regs[24];
  m->stop_sp = rt_spill_regs(regs);
  m->stop_hi = m->curg->stack_hi;
  uint64_t before = atomic_load(&heap_live);
  mcache_flush_all();
  int64_t t1 = os_nanotime();
  mark_roots();
  if (weak_live) weak_clear();
  bool queued = mark_finalizers();
  memprof_sweep(is_marked);
  int64_t t2 = os_nanotime();
  sweep_all();
  regions_trim();
  int64_t t3 = os_nanotime();
  uint64_t live = atomic_load(&heap_live);
  last_live = live;
  atomic_store(&gc_alloc_mark, atomic_load(&stat_total_alloc));
  if (gc_percent >= 0) {
    uint64_t goal = live + live * (uint64_t)gc_percent / 100;
    next_gc = goal < (4 << 20) ? (4 << 20) : goal;
  } else {
    next_gc = UINT64_MAX;
  }
  for (int32_t i = 0; i <= gc_nhelpers; i++) {
    gcw *w = &workers[i];
    if (w->cap > (1 << 20)) {  // drop a large mark stack
      os_unmap(w->stk, w->cap * sizeof *w->stk);
      w->stk = NULL;
      w->cap = 0;
    }
  }
  atomic_fetch_add(&num_gc, 1);
  int64_t t4 = os_nanotime();
  pause_total_ns += (uint64_t)(t4 - t0);
  if (UNLIKELY(gc_trace)) gc_print_trace(before, live, t0, t1, t2, t3, t4);
  if (UNLIKELY(gc_trace >= 2)) gc_print_classes();
  if (UNLIKELY(gc_trace >= 3)) print_roots();
  bool wake = false;
  if (queued) {
    spin_lock(&fin_lock);
    if (fing_parked && finqlen) {
      fing_parked = false;
      wake = true;
    }
    spin_unlock(&fin_lock);
  }
  start_the_world();
  __asm__ volatile("" ::"r"(regs) : "memory");
  rt_unlock(&gc_mu);
  if (wake) rt_ready(fing);
}

RT_API void rt_gc_collect(void) { gc_collect(true); }

void gc_parallel_enable(void) {
  if (__atomic_load_n(&helpers_init_fn, __ATOMIC_ACQUIRE)) return;
  __atomic_store_n(&drain_fn, drain_parallel, __ATOMIC_RELEASE);
  __atomic_store_n(&helpers_init_fn, helpers_init, __ATOMIC_RELEASE);
}

// Exact counts: allocations still pending in per-P caches are added in with
// the world stopped (like Go's ReadMemStats).
RT_API void rt_gc_stats(rt_memstats *out) {
  uint64_t pm = 0, pb = 0, pf = 0;
  if (getm() && getm()->p) {
    stop_the_world();
    mcache_pending(&pm, &pb, &pf);
    start_the_world();
  }
  out->heap_alloc = atomic_load(&heap_live);
  out->heap_sys = heap_sys;
  out->total_alloc = atomic_load(&stat_total_alloc) + pb;
  out->mallocs = atomic_load(&stat_mallocs) + pm;
  out->frees = atomic_load(&stat_frees) + pf;
  out->num_gc = atomic_load(&num_gc);
  out->pause_total_ns = pause_total_ns;
  out->last_live = last_live;
  out->heap_released = heap_released;
}

RT_API int32_t rt_gc_set_percent(int32_t percent) {
  rt_lock(&gc_mu);
  int32_t old = gc_percent;
  gc_percent = percent < 0 ? -1 : percent;
  if (gc_percent < 0) {
    next_gc = UINT64_MAX;
  } else {
    uint64_t live = atomic_load(&heap_live);
    uint64_t goal = live + live * (uint64_t)gc_percent / 100;
    next_gc = goal < (4 << 20) ? (4 << 20) : goal;
  }
  rt_unlock(&gc_mu);
  return old;
}

void gc_init(void) {
  const char *t = rt_getenv("MARMOT_GCTRACE");
  gc_trace = t && *t >= '1' && *t <= '9' ? *t - '0' : 0;
  if (gc_percent < 0) next_gc = UINT64_MAX;
}

// Debug helper for tests: describe the heap object containing p.
void gc_debug_object(void *ptr) {
  uintptr_t p = (uintptr_t)ptr;
  rt_print("object ");
  rt_printx(p);
  if (p - rt_arena_start >= rt_arena_used - rt_arena_start) {
    rt_print(" not in arena\n");
    return;
  }
  mspan *s = rt_pagemap[(p - rt_arena_start) >> PAGE_SHIFT];
  if (!s) {
    rt_print(" no span\n");
    return;
  }
  rt_print(" span base=");
  rt_printx(s->base);
  rt_print(" npages=");
  rt_printu(s->npages);
  rt_print(" state=");
  rt_printu(s->state);
  rt_print(" cls=");
  rt_printu(s->cls);
  rt_print(" kind=");
  rt_printu(s->kind);
  rt_print(" nelems=");
  rt_printu(s->nelems);
  rt_print(" freeindex=");
  rt_printu(s->freeindex);
  rt_print(" cached=");
  rt_printu(s->cached);
  if (s->state == SPAN_INUSE && s->cls) {
    uintptr_t idx = (p - s->base) / s->elemsize;
    rt_print(" idx=");
    rt_printu(idx);
    rt_print(" alloc=");
    rt_printu((s->allocbits[idx / 64] >> (idx % 64)) & 1);
  }
  rt_print("\n");
}
