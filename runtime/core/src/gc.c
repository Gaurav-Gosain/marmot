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
static _Atomic uint64_t num_gc;
static uint64_t pause_total_ns;
static uintptr_t *mstack;
static uintptr_t mlen, mcap;

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

static void mstack_push(uintptr_t obj) {
  if (mlen == mcap) {
    uintptr_t ncap = mcap ? mcap * 2 : 65536;
    uintptr_t *n = os_map(ncap * sizeof *n);
    if (!n) rt_throw("out of memory (mark stack)");
    if (mlen) __builtin_memcpy(n, mstack, mlen * sizeof *n);
    if (mstack) os_unmap(mstack, mcap * sizeof *n);
    mstack = n;
    mcap = ncap;
  }
  mstack[mlen++] = obj;
}

// Find the object containing p. Returns its span and index, or NULL.
INLINE mspan *find_object(uintptr_t p, uintptr_t *idxp) {
  if (p - rt_arena_start >= rt_arena_used - rt_arena_start) return NULL;
  mspan *s = rt_pagemap[(p - rt_arena_start) >> PAGE_SHIFT];
  if (!s || s->state != SPAN_INUSE || p < s->base || p >= s->limit) return NULL;
  uintptr_t idx = s->cls ? (p - s->base) / s->elemsize : 0;
  if (idx >= s->nelems) return NULL;
  if (!((s->allocbits[idx / 64] >> (idx % 64)) & 1)) return NULL;  // free slot
  *idxp = idx;
  return s;
}

INLINE void mark_ptr(uintptr_t p) {
  uintptr_t idx;
  mspan *s = find_object(p, &idx);
  if (!s) return;
  uint64_t bit = 1ull << (idx % 64);
  uint64_t *w = &s->markbits[idx / 64];
  if (*w & bit) return;
  *w |= bit;
  if (s->kind != RT_ALLOC_NOSCAN) mstack_push(s->base + idx * s->elemsize);
}

void gc_scan_range(uintptr_t lo, uintptr_t hi) {
  lo = (lo + 7) & ~(uintptr_t)7;
  for (uintptr_t p = lo; p + 8 <= hi; p += 8) mark_ptr(*(uintptr_t *)p);
}

static void scan_object(uintptr_t obj) {
  mspan *s = rt_pagemap[(obj - rt_arena_start) >> PAGE_SHIFT];
  if (s->kind == RT_ALLOC_CONSERVATIVE) {
    gc_scan_range(obj, obj + s->elemsize);
  } else if (s->cls) {
    // Small precise object: heap bits.
    uintptr_t w0 = (obj - rt_arena_start) / 8;
    uintptr_t nw = s->elemsize / 8;
    for (uintptr_t i = 0; i < nw;) {
      uintptr_t w = w0 + i;
      uint64_t bits = rt_heapbits[w / 64] >> (w % 64);
      uintptr_t n = 64 - w % 64;
      if (n > nw - i) {
        n = nw - i;
        bits &= (1ull << n) - 1;
      }
      while (bits) {
        uintptr_t b = (uintptr_t)__builtin_ctzll(bits);
        bits &= bits - 1;
        mark_ptr(((uintptr_t *)obj)[i + b]);
      }
      i += n;
    }
  } else {
    // Large precise object: repeat the element ptrmap.
    uintptr_t ew = s->ptrelem / 8;
    const uint8_t *pm = s->ptrmap;
    for (uintptr_t e = 0; e < s->ptrcount; e++) {
      uintptr_t *elem = (uintptr_t *)(obj + e * s->ptrelem);
      for (uintptr_t i = 0; i < ew; i++)
        if ((pm[i / 8] >> (i % 8)) & 1) mark_ptr(elem[i]);
    }
  }
}

static void drain(void) {
  while (mlen > 0) scan_object(mstack[--mlen]);
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

static void scan_data_segment(void *lo, void *hi) { gc_scan_range((uintptr_t)lo, (uintptr_t)hi); }

static void mark_roots(void) {
  os_data_segments(scan_data_segment);
  for (int32_t i = 0; i < nroots; i++) gc_scan_range(roots[i].lo, roots[i].hi);
  sched_scan_roots(gc_scan_range);
  timers_scan_roots(gc_scan_range);
  for (uintptr_t i = 0; i < finqlen; i++) mark_ptr(finq[i].obj);
  drain();
}

static bool mark_finalizers(void) {
  bool queued = false;
  if (!finlen) return false;
  // Referents of finalizable objects stay alive.
  for (uintptr_t i = 0; i < fincap; i++) {
    uintptr_t obj = fintab[i].obj;
    if (obj <= 1 || is_marked(obj)) continue;
    uintptr_t idx;
    mspan *s = find_object(obj, &idx);
    if (s && s->kind != RT_ALLOC_NOSCAN) {
      scan_object(s->base + idx * s->elemsize);
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

static void sweep_all(void) {
  spin_lock(&heap_lock);
  central_reset();
  uint64_t freed_bytes = 0, freed_objs = 0;
  for (int32_t i = nallspans - 1; i >= 0; i--) {
    mspan *s = allspans[i];
    uint32_t nw = (s->nelems + 63) / 64;
    uintptr_t freed = 0, live = 0;
    for (uint32_t w = 0; w < nw; w++) {
      freed += popcount64(s->allocbits[w] & ~s->markbits[w]);
      live += popcount64(s->markbits[w]);
    }
    freed_bytes += freed * s->elemsize;
    freed_objs += freed;
    uint64_t *t = s->allocbits;
    s->allocbits = s->markbits;
    s->markbits = t;
    __builtin_memset(t, 0, nw * 8);
    s->freeindex = 0;
    s->alloc_cache = 0;
    if (live == 0) {
      span_free(s);
    } else if (s->cls) {
      s->needzero = 1;
      central_put(s);
    }
  }
  atomic_fetch_sub(&heap_live, freed_bytes);
  atomic_fetch_add(&stat_frees, freed_objs);
  heap_scavenge();
  spin_unlock(&heap_lock);
}

// ---- collection ----

void gc_collect(bool forced) {
  uint64_t gen = atomic_load(&num_gc);
  rt_lock(&gc_mu);
  if (!forced && (atomic_load(&num_gc) != gen || atomic_load(&heap_live) < next_gc)) {
    rt_unlock(&gc_mu);
    return;
  }
  int64_t t0 = os_nanotime();
  stop_the_world();
  rt_m *m = getm();
  void *regs[24];
  m->stop_sp = rt_spill_regs(regs);
  m->stop_hi = m->curg->stack_hi;
  mcache_flush_all();
  mark_roots();
  bool queued = mark_finalizers();
  sweep_all();
  uint64_t live = atomic_load(&heap_live);
  if (gc_percent >= 0) {
    uint64_t goal = live + live * (uint64_t)gc_percent / 100;
    next_gc = goal < (4 << 20) ? (4 << 20) : goal;
  } else {
    next_gc = UINT64_MAX;
  }
  if (mcap > (1 << 20)) {  // drop a large mark stack
    os_unmap(mstack, mcap * sizeof *mstack);
    mstack = NULL;
    mcap = 0;
  }
  atomic_fetch_add(&num_gc, 1);
  pause_total_ns += (uint64_t)(os_nanotime() - t0);
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

RT_API void rt_gc_stats(rt_memstats *out) {
  out->heap_alloc = atomic_load(&heap_live);
  out->heap_sys = heap_sys;
  out->total_alloc = atomic_load(&stat_total_alloc);
  out->mallocs = atomic_load(&stat_mallocs);
  out->frees = atomic_load(&stat_frees);
  out->num_gc = atomic_load(&num_gc);
  out->pause_total_ns = pause_total_ns;
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
