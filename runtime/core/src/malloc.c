// malloc.c: the heap. Size classes, per-P caches, central span lists, page heap.
//
// All heap memory comes from one reserved arena, committed as it grows, so a
// pointer is a heap pointer iff it is in [rt_arena_start, rt_arena_used) and
// its page maps to an in-use span (rt_pagemap). Small objects (<= 32 KiB) live
// in spans of one size class and one kind (noscan, conservative, precise);
// large objects get their own span. Allocation state per span is Go's:
// allocbits plus freeindex, with a 64-bit alloc_cache of free bits so the
// fast path is a count-trailing-zeros and two stores. Precise objects record
// their pointer words in rt_heapbits, one bit per heap word.
//
// Stop-the-world safety: the fast path runs with m->locks raised, so an M is
// never stopped half way through updating a span. The GC flushes all caches
// while the world is stopped.

#include "malloc.h"

uint8_t rt_size_to_class8[1024 / 8 + 1];
uint8_t rt_size_to_class128[(MAX_SMALL - 1024) / 128 + 1];
uint32_t rt_class_size[NUM_CLASSES];
static uint8_t class_npages[NUM_CLASSES];

uintptr_t rt_arena_start, rt_arena_used, rt_arena_end;
mspan **rt_pagemap;
uint64_t *rt_heapbits;
static uintptr_t meta_committed;  // arena bytes whose pagemap and heapbits are committed

rt_spin heap_lock;
mspan **allspans;
int32_t nallspans;
static int32_t capallspans;
_Atomic uint64_t heap_live;
uint64_t heap_sys, heap_released;
_Atomic uint64_t stat_mallocs, stat_total_alloc, stat_frees;
uint64_t next_gc = 4 << 20;
int32_t gc_percent = 100;
int64_t rt_gcstress;
static _Atomic int64_t gcstress_count;

static mspan empty_span;  // alloc_cache 0: always takes the slow path
static fixalloc span_alloc;
static fixalloc bits_alloc[5];  // 1, 2, 4, 8, 16 words
static mcache *all_mcaches[257];
static int32_t nmcaches;
static uint8_t zerobase[16];

// Central lists per span class: spans with free objects and full spans.
static mspan *central_partial[NUM_SPANCLASS];
static mspan *central_full[NUM_SPANCLASS];

// Page heap free lists: exact sizes below 128 pages, one list for larger.
#define FREE_EXACT 128
static mspan *free_exact[FREE_EXACT];
static mspan *free_large;

// ---- size classes ----

// Object size and span bytes per class, as in the gc runtime.
static const uint32_t class_def[NUM_CLASSES][2] = {
    {0, 0},          {8, 8192},       {16, 8192},     {24, 8192},     {32, 8192},
    {48, 8192},      {64, 8192},      {80, 8192},     {96, 8192},     {112, 8192},
    {128, 8192},     {144, 8192},     {160, 8192},    {176, 8192},    {192, 8192},
    {208, 8192},     {224, 8192},     {240, 8192},    {256, 8192},    {288, 8192},
    {320, 8192},     {352, 8192},     {384, 8192},    {416, 8192},    {448, 8192},
    {480, 8192},     {512, 8192},     {576, 8192},    {640, 8192},    {704, 8192},
    {768, 8192},     {896, 8192},     {1024, 8192},   {1152, 8192},   {1280, 8192},
    {1408, 16384},   {1536, 8192},    {1792, 16384},  {2048, 8192},   {2304, 16384},
    {2688, 8192},    {3072, 24576},   {3200, 16384},  {3456, 24576},  {4096, 8192},
    {4864, 24576},   {5376, 16384},   {6144, 24576},  {6528, 32768},  {6784, 40960},
    {6912, 49152},   {8192, 8192},    {9472, 57344},  {9728, 49152},  {10240, 40960},
    {10880, 32768},  {12288, 24576},  {13568, 40960}, {14336, 57344}, {16384, 16384},
    {18432, 73728}, {19072, 57344}, {20480, 40960}, {21760, 65536}, {24576, 24576},
    {27264, 81920}, {28672, 57344}, {32768, 32768},
};

static void classes_init(void) {
  for (int c = 1; c < NUM_CLASSES; c++) {
    rt_class_size[c] = class_def[c][0];
    class_npages[c] = (uint8_t)(class_def[c][1] / PAGE_SIZE);
  }
  int c = 1;
  for (uint32_t s = 0; s <= 1024; s += 8) {
    while (rt_class_size[c] < s) c++;
    rt_size_to_class8[s / 8] = (uint8_t)c;
  }
  for (uint32_t s = 1024; s <= MAX_SMALL; s += 128) {
    while (rt_class_size[c] < s) c++;
    rt_size_to_class128[(s - 1024) / 128] = (uint8_t)c;
  }
}

// ---- metadata ----

static uint64_t *bits_new(uint32_t nwords) {
  int k = 0;
  while ((1u << k) < nwords) k++;
  return fixalloc_alloc(&bits_alloc[k]);
}

static void bits_free(uint64_t *b, uint32_t nwords) {
  int k = 0;
  while ((1u << k) < nwords) k++;
  fixalloc_free(&bits_alloc[k], b);
}

static void allspans_add(mspan *s) {
  if (nallspans == capallspans) {
    int32_t ncap = capallspans ? capallspans * 2 : 1024;
    mspan **n = os_map((uintptr_t)ncap * sizeof *n);
    if (!n) rt_throw("out of memory (allspans)");
    if (nallspans) __builtin_memcpy(n, allspans, (uintptr_t)nallspans * sizeof *n);
    if (allspans) os_unmap(allspans, (uintptr_t)capallspans * sizeof *n);
    allspans = n;
    capallspans = ncap;
  }
  s->allidx = nallspans;
  allspans[nallspans++] = s;
}

static void allspans_remove(mspan *s) {
  int32_t i = s->allidx;
  mspan *last = allspans[--nallspans];
  allspans[i] = last;
  last->allidx = i;
  s->allidx = -1;
}

// ---- arena ----

static void arena_init(void) {
#if RT_ARCH_WASM32
  // wasm32: 4 GiB of linear memory in all, and a reservation is memory
  // grown at once (zeroed, backed by the engine only as it is touched).
  uintptr_t size = (uintptr_t)1 << 30;
#else
  uintptr_t size = (uintptr_t)64 << 30;
#endif
  void *p = NULL;
  while (size >= ((uintptr_t)256 << 20)) {
    p = os_reserve(size);
    if (p) break;
    size >>= 1;
  }
  if (!p) rt_throw("cannot reserve heap arena");
  // Align the start to a page so page numbers are exact.
  uintptr_t start = ((uintptr_t)p + PAGE_SIZE - 1) & ~(PAGE_SIZE - 1);
  rt_arena_start = start;
  rt_arena_end = (uintptr_t)p + size;
  rt_arena_used = start;
  uintptr_t npages = (rt_arena_end - start) >> PAGE_SHIFT;
  rt_pagemap = os_reserve(npages * sizeof(mspan *) + rt_pagesize);
  rt_heapbits = os_reserve((rt_arena_end - start) / (8 * WORDSZ) + rt_pagesize);
  if (!rt_pagemap || !rt_heapbits) rt_throw("cannot reserve heap metadata");
}

static void meta_commit(uintptr_t upto) {
  if (upto <= meta_committed) return;
  uintptr_t ps = rt_pagesize;
  uintptr_t pm_lo = (meta_committed >> PAGE_SHIFT) * sizeof(mspan *);
  uintptr_t pm_hi = (upto >> PAGE_SHIFT) * sizeof(mspan *);
  pm_lo &= ~(ps - 1);
  pm_hi = (pm_hi + ps - 1) & ~(ps - 1);
  if (!os_commit((uint8_t *)rt_pagemap + pm_lo, pm_hi - pm_lo)) rt_throw("out of memory (pagemap)");
  uintptr_t hb_lo = (meta_committed / (8 * WORDSZ)) & ~(ps - 1);
  uintptr_t hb_hi = (upto / (8 * WORDSZ) + ps - 1) & ~(ps - 1);
  if (hb_hi > hb_lo && !os_commit((uint8_t *)rt_heapbits + hb_lo, hb_hi - hb_lo))
    rt_throw("out of memory (heapbits)");
  meta_committed = upto;
}

INLINE void pagemap_set(uintptr_t base, uintptr_t npages, mspan *s) {
  uintptr_t i = (base - rt_arena_start) >> PAGE_SHIFT;
  for (uintptr_t k = 0; k < npages; k++) rt_pagemap[i + k] = s;
}

INLINE mspan *pagemap_get(uintptr_t addr) {
  return rt_pagemap[(addr - rt_arena_start) >> PAGE_SHIFT];
}

// ---- page heap (heap_lock held) ----

static void freelist_insert(mspan *s) {
  mspan **head = s->npages < FREE_EXACT ? &free_exact[s->npages] : &free_large;
  s->prev = NULL;
  s->next = *head;
  if (*head) (*head)->prev = s;
  *head = s;
}

static void freelist_remove(mspan *s) {
  mspan **head = s->npages < FREE_EXACT ? &free_exact[s->npages] : &free_large;
  if (s->prev)
    s->prev->next = s->next;
  else
    *head = s->next;
  if (s->next) s->next->prev = s->prev;
  s->next = s->prev = NULL;
}

// Retire a span structure. Stale page map entries may still point to it, so
// mark it dead: it must never look like a free or in-use span again.
static void span_struct_free(mspan *s) {
  s->state = SPAN_DEAD;
  fixalloc_free(&span_alloc, s);
}

// A merged span is released only if both halves were; a released half
// merged into an unreleased one counts as backed again (its pages are
// released again by the next scavenge, which counts them then).
static void merge_released(mspan *s, mspan *o) {
  if (s->released != o->released) heap_released -= (s->released ? s->npages : o->npages) * PAGE_SIZE;
  s->released &= o->released;
}

// Insert a free span, merging with free neighbors. The page map entries on
// both sides of s must be current (they are boundaries of other spans).
static void free_span_insert(mspan *s) {
  s->state = SPAN_FREE;
  if (s->base > rt_arena_start) {
    mspan *l = pagemap_get(s->base - 1);
    if (l && l->state == SPAN_FREE && l->limit == s->base) {
      freelist_remove(l);
      merge_released(s, l);
      s->base = l->base;
      s->npages += l->npages;
      s->needzero |= l->needzero;
      span_struct_free(l);
    }
  }
  if (s->limit < rt_arena_used) {
    mspan *r = pagemap_get(s->limit);
    if (r && r->state == SPAN_FREE && r->base == s->limit) {
      freelist_remove(r);
      merge_released(s, r);
      s->npages += r->npages;
      s->needzero |= r->needzero;
      span_struct_free(r);
    }
  }
  s->limit = s->base + s->npages * PAGE_SIZE;
  // Only the end pages need to map to a free span (for merging).
  rt_pagemap[(s->base - rt_arena_start) >> PAGE_SHIFT] = s;
  rt_pagemap[(s->limit - 1 - rt_arena_start) >> PAGE_SHIFT] = s;
  freelist_insert(s);
}

static void arena_grow(uintptr_t npages) {
  uintptr_t n = npages * PAGE_SIZE;
  uintptr_t chunk = 4 << 20;
  if (n < chunk) n = chunk;
  n = (n + rt_pagesize - 1) & ~(rt_pagesize - 1);
  if (rt_arena_used + n > rt_arena_end) {
    n = npages * PAGE_SIZE;
    if (rt_arena_used + n > rt_arena_end) rt_throw("out of memory: heap arena exhausted");
  }
  if (!os_commit((void *)rt_arena_used, n)) rt_throw("out of memory");
  uintptr_t base = rt_arena_used;
  rt_arena_used += n;
  meta_commit(rt_arena_used - rt_arena_start);
  heap_sys += n;
  mspan *s = fixalloc_alloc(&span_alloc);
  s->base = base;
  s->npages = n / PAGE_SIZE;
  s->limit = base + n;
  s->allidx = -1;
  free_span_insert(s);
}

// Allocate npages of pages as a new in-use span (not yet initialized).
static mspan *page_alloc(uintptr_t npages) {
  mspan *s = NULL;
  for (;;) {
    for (uintptr_t k = npages; k < FREE_EXACT && !s; k++)
      if (free_exact[k]) s = free_exact[k];
    if (!s) {
      // Best fit among large free spans.
      for (mspan *x = free_large; x; x = x->next)
        if (x->npages >= npages && (!s || x->npages < s->npages || (x->npages == s->npages && x->base < s->base))) s = x;
    }
    if (s) break;
    arena_grow(npages);
  }
  freelist_remove(s);
  s->state = SPAN_INUSE;
  if (s->npages > npages) {
    mspan *rest = fixalloc_alloc(&span_alloc);
    rest->base = s->base + npages * PAGE_SIZE;
    rest->npages = s->npages - npages;
    rest->limit = s->limit;
    rest->needzero = s->needzero;
    rest->released = s->released;
    rest->allidx = -1;
    s->npages = npages;
    s->limit = rest->base;
    // Map s first: rest's left neighbor lookup reads s's last page.
    pagemap_set(s->base, s->npages, s);
    free_span_insert(rest);
  } else {
    pagemap_set(s->base, s->npages, s);
  }
  if (s->released) {
    heap_released -= s->npages * PAGE_SIZE;
    s->released = 0;
  }
  allspans_add(s);
  return s;
}

void span_free(mspan *s) {
  allspans_remove(s);
  if (s->allocbits) {
    uint32_t nw = (s->nelems + 63) / 64;
    bits_free(s->allocbits, nw);
    bits_free(s->markbits, nw);
    s->allocbits = s->markbits = NULL;
  }
  pagemap_set(s->base, s->npages, NULL);
  s->needzero = 1;
  s->cached = 0;
  s->cls = 0;
  s->kind = 0;
  s->ptrmap = NULL;
  s->finalizers = 0;
  free_span_insert(s);
}

// Return free memory to the OS after a GC, keeping up to retain bytes of
// free pages backed (the heap goal): a program that frees and allocates
// the same amount every cycle reuses its pages instead of faulting them in
// and clearing them again after each collection. The largest spans go
// first. Spans under 64 KiB are kept.
void heap_scavenge(uint64_t retain) { heap_scavenge_min(retain, 64 << 10); }

// heap_scavenge with spans of min bytes or more released (the idle
// collection releases down to the OS page).
void heap_scavenge_min(uint64_t retain, uintptr_t min) {
  if (min < rt_pagesize) min = rt_pagesize;
  uint64_t idle = 0;
  for (mspan *s = free_large; s; s = s->next)
    if (!s->released) idle += s->npages * PAGE_SIZE;
  for (uintptr_t k = 0; k < FREE_EXACT; k++)
    for (mspan *s = free_exact[k]; s; s = s->next)
      if (!s->released) idle += s->npages * PAGE_SIZE;
  if (idle <= retain) return;
  for (uintptr_t k = FREE_EXACT + 1; k-- > 0 && idle > retain;) {
    for (mspan *s = k == FREE_EXACT ? free_large : free_exact[k]; s && idle > retain; s = s->next) {
      uintptr_t n = s->npages * PAGE_SIZE;
      if (s->released || n < min) continue;
      os_release((void *)s->base, n);  // whole OS pages only
      s->released = 1;
      s->needzero = 1;
      heap_released += n;
      idle -= n;
    }
  }
}

// ---- central lists (heap_lock held) ----

void central_reset(void) {
  __builtin_memset(central_partial, 0, sizeof central_partial);
  __builtin_memset(central_full, 0, sizeof central_full);
}

void central_put(mspan *s) {
  uint32_t spc = s->cls * NUM_KINDS + s->kind;
  uintptr_t nfree = 0;
  uint32_t nw = (s->nelems + 63) / 64;
  for (uint32_t i = 0; i < nw; i++) nfree += popcount64(~s->allocbits[i]);
  nfree -= nw * 64 - s->nelems;  // tail bits beyond nelems are zero
  mspan **head = nfree ? &central_partial[spc] : &central_full[spc];
  s->next = *head;
  *head = s;
}

static mspan *span_new_small(uint32_t cls, uint32_t kind) {
  mspan *s = page_alloc(class_npages[cls]);
  s->cls = (uint8_t)cls;
  s->kind = (uint8_t)kind;
  s->elemsize = rt_class_size[cls];
  s->nelems = (uint32_t)((s->npages * PAGE_SIZE) / s->elemsize);
  uint32_t nw = (s->nelems + 63) / 64;
  s->allocbits = bits_new(nw);
  s->markbits = bits_new(nw);
  s->freeindex = 0;
  s->divmul = (uint32_t)(0xffffffffu / (uint32_t)s->elemsize + 1);
  return s;
}

// Fill alloc_cache from allocbits starting at freeindex (a multiple of 64).
INLINE void refill_alloc_cache(mspan *s) {
  uint32_t w = s->freeindex / 64;
  s->alloc_cache = ~s->allocbits[w];
}

// Find the next free index at or after freeindex, updating alloc_cache.
// Returns nelems if the span is full.
static uint32_t next_free_index(mspan *s) {
  uint32_t fi = s->freeindex;
  uint32_t n = s->nelems;
  if (fi == n) return n;
  uint64_t cache = s->alloc_cache;
  for (;;) {
    if (cache) {
      uint32_t b = (uint32_t)__builtin_ctzll(cache);
      uint32_t r = fi + b;
      if (r >= n) {
        s->freeindex = n;
        s->alloc_cache = 0;
        return n;
      }
      s->alloc_cache = b == 63 ? 0 : cache >> (b + 1);
      s->freeindex = r + 1;
      if (s->freeindex % 64 == 0 && s->freeindex != n) refill_alloc_cache(s);
      return r;
    }
    // Move to the next 64-bit word.
    fi = (fi + 64) & ~63u;
    if (fi >= n) {
      s->freeindex = n;
      s->alloc_cache = 0;
      return n;
    }
    s->freeindex = fi;
    refill_alloc_cache(s);
    cache = s->alloc_cache;
  }
}

// Objects of cached span s handed out since it was cached.
static uint32_t span_handed_out(mspan *s) {
  uint32_t left = 0;
  if (s->freeindex < s->nelems) {
    uint32_t fi = s->freeindex;
    uint32_t nw = (s->nelems + 63) / 64;
    for (uint32_t w = fi / 64; w < nw; w++) {
      uint64_t free = ~s->allocbits[w];
      if (w == fi / 64) free &= ~0ull << (fi % 64);
      if (w == nw - 1 && s->nelems % 64) free &= (1ull << (s->nelems % 64)) - 1;
      left += (uint32_t)popcount64(free);
    }
  }
  return s->nfree_cached - left;
}

void mcache_pending(uint64_t *mallocs, uint64_t *bytes, uint64_t *frees) {
  uint64_t n = 0, b = 0, f = 0;
  spin_lock(&heap_lock);
  for (int32_t i = 0; i < nmcaches; i++) {
    mcache *c = all_mcaches[i];
    for (int j = 0; j < NUM_SPANCLASS; j++) {
      mspan *s = c->e[j].span;
      if (s == &empty_span) continue;
      uint32_t u = span_handed_out(s);
      n += u;
      b += (uint64_t)u * s->elemsize;
    }
    n += c->nreuse;
    b += c->reuse_bytes;
    f += c->nreuse;
  }
  spin_unlock(&heap_lock);
  *mallocs = n;
  *bytes = b;
  *frees = f;
}

// Return a cached span to the central lists. heap_lock held.
static void mcache_release(mspan *s) {
  // Objects at or after freeindex that are still free were never handed out.
  uint32_t left = 0;
  if (s->freeindex < s->nelems) {
    uint32_t fi = s->freeindex;
    uint32_t nw = (s->nelems + 63) / 64;
    for (uint32_t w = fi / 64; w < nw; w++) {
      uint64_t free = ~s->allocbits[w];
      if (w == fi / 64) free &= ~0ull << (fi % 64);
      if (w == nw - 1 && s->nelems % 64) free &= (1ull << (s->nelems % 64)) - 1;
      left += (uint32_t)popcount64(free);
    }
  }
  uint32_t used = s->nfree_cached - left;
  atomic_fetch_add(&stat_mallocs, used);
  atomic_fetch_add(&stat_total_alloc, (uint64_t)used * s->elemsize);
  atomic_fetch_sub(&heap_live, (uint64_t)left * s->elemsize);
  // Fold the cache's progress into allocbits: everything below freeindex
  // is allocated.
  uint32_t fi = s->freeindex;
  for (uint32_t w = 0; w < fi / 64; w++) s->allocbits[w] = ~0ull;
  if (fi % 64) s->allocbits[fi / 64] |= (1ull << (fi % 64)) - 1;
  uint32_t nw = (s->nelems + 63) / 64;
  if (s->nelems % 64) s->allocbits[nw - 1] &= (1ull << (s->nelems % 64)) - 1;
  s->freeindex = 0;
  s->alloc_cache = 0;
  s->cached = 0;
}

// Take a span with free objects for spanclass spc into cache c.
static void mcache_refill(mcache *c, uint32_t spc) {
  uint32_t cls = spc / NUM_KINDS, kind = spc % NUM_KINDS;
  mspan *old = c->e[spc].span;
  spin_lock(&heap_lock);
  if (old != &empty_span) {
    // The cached span is exhausted: fold its state and retire it.
    mcache_release(old);
    old->next = central_full[spc];
    central_full[spc] = old;
  }
  mspan *s = central_partial[spc];
  if (s) {
    central_partial[spc] = s->next;
    s->next = NULL;
  } else {
    s = span_new_small(cls, kind);
  }
  s->freeindex = 0;
  refill_alloc_cache(s);
  uint32_t nw = (s->nelems + 63) / 64;
  uint32_t nfree = 0;
  for (uint32_t i = 0; i < nw; i++) nfree += (uint32_t)popcount64(~s->allocbits[i]);
  nfree -= nw * 64 - s->nelems;
  s->nfree_cached = nfree;
  s->cached = 1;
  spin_unlock(&heap_lock);
  atomic_fetch_add(&heap_live, (uint64_t)nfree * s->elemsize);
  c->e[spc].span = s;
}

mcache *mcache_new(void) {
  mcache *c = persistent_alloc(sizeof(mcache), 64);
  for (int i = 0; i < NUM_SPANCLASS; i++) c->e[i].span = &empty_span;
  spin_lock(&heap_lock);
  all_mcaches[nmcaches++] = c;
  spin_unlock(&heap_lock);
  return c;
}

// World stopped. Return all cached spans; central lists are rebuilt by the
// sweep that follows.
void mcache_flush_all(void) {
  spin_lock(&heap_lock);
  for (int32_t i = 0; i < nmcaches; i++) {
    mcache *c = all_mcaches[i];
    for (int k = 0; k < NUM_SPANCLASS; k++) {
      mspan *s = c->e[k].span;
      if (s != &empty_span) {
        mcache_release(s);
        c->e[k].span = &empty_span;
      }
      c->e[k].freed = NULL;  // garbage to the GC; the sweep reclaims it
    }
  }
  // Spans on central lists also keep allocbits exact already.
  spin_unlock(&heap_lock);
}

// ---- allocation ----

void rt_heapbits_set(uintptr_t p, uintptr_t size, uintptr_t clsize, const uint8_t *ptrmap,
                     uintptr_t elemsize) {
  static const uint8_t all_ptrs[1] = {1};
  if (!ptrmap || elemsize == 0 || elemsize % WORDSZ != 0) {
    // Invalid precise request: every word may be a pointer.
    ptrmap = all_ptrs;
    elemsize = WORDSZ;
    size = clsize;
  }
  uintptr_t w0 = (p - rt_arena_start) / WORDSZ;
  uintptr_t nw = clsize / WORDSZ;
  uintptr_t ew = elemsize / WORDSZ;
  uintptr_t used = (size / elemsize) * ew;  // words covered by whole elements
  uintptr_t e = 0;                          // word index within the element
  for (uintptr_t i = 0; i < nw;) {
    uintptr_t w = w0 + i;
    uintptr_t shift = w % 64;
    uintptr_t n = 64 - shift;
    if (n > nw - i) n = nw - i;
    uint64_t bits = 0;
    for (uintptr_t k = 0; k < n; k++, i++) {
      if (i < used && ((ptrmap[e / 8] >> (e % 8)) & 1)) bits |= 1ull << k;
      if (++e == ew) e = 0;
    }
    uint64_t mask = n == 64 ? ~0ull : ((1ull << n) - 1) << shift;
    rt_heapbits[w / 64] = (rt_heapbits[w / 64] & ~mask) | (bits << shift);
  }
}

static void *alloc_large(uintptr_t size, uint32_t kind, const uint8_t *ptrmap,
                         uintptr_t elemsize, bool zero) {
  uintptr_t npages = (size + PAGE_SIZE - 1) >> PAGE_SHIFT;
  if (npages == 0 || npages > (rt_arena_end - rt_arena_start) >> PAGE_SHIFT)
    rt_throw("out of memory: allocation size too large");
  spin_lock(&heap_lock);
  mspan *s = page_alloc(npages);
  s->cls = 0;
  s->kind = (uint8_t)kind;
  s->elemsize = npages * PAGE_SIZE;
  s->nelems = 1;
  s->allocbits = bits_new(1);
  s->markbits = bits_new(1);
  s->allocbits[0] = 1;
  s->freeindex = 1;
  if (kind == RT_ALLOC_PRECISE) {
    s->ptrmap = ptrmap;
    s->ptrelem = elemsize;
    s->ptrcount = size / elemsize;
  }
  bool dirty = s->needzero;
  s->needzero = 0;
  spin_unlock(&heap_lock);
  if (dirty && zero) __builtin_memset((void *)s->base, 0, size);
  if (dirty && !zero) s->needzero = 1;  // not cleared here: the next owner of these pages must
  atomic_fetch_add(&heap_live, s->elemsize);
  atomic_fetch_add(&stat_mallocs, 1);
  atomic_fetch_add(&stat_total_alloc, s->elemsize);
  return (void *)s->base;
}

INLINE uint32_t size_class(uintptr_t size) {
  return size <= 1024 ? rt_size_to_class8[(size + 7) >> 3]
                      : rt_size_to_class128[(size - 1024 + 127) >> 7];
}

// Allocate from the span cached for spc, refilling as needed. m->locks held.
static void *alloc_small_locked(mcache *c, uint32_t spc, uintptr_t size,
                                const uint8_t *ptrmap, uintptr_t elemsize, bool zero) {
  mspan *s = c->e[spc].span;
  uint32_t idx = s == &empty_span ? 0 : next_free_index(s);
  if (s == &empty_span || idx == s->nelems) {
    mcache_refill(c, spc);
    s = c->e[spc].span;
    idx = next_free_index(s);
    if (idx == s->nelems) rt_throw("mcache_refill: span has no free object");
  }
  uintptr_t p = s->base + idx * s->elemsize;
  if (s->needzero && zero) __builtin_memset((void *)p, 0, s->elemsize);
  if (s->kind == RT_ALLOC_PRECISE) rt_heapbits_set(p, size, s->elemsize, ptrmap, elemsize);
  return (void *)p;
}

static void *alloc_slow(uintptr_t size, uint32_t kind, const uint8_t *ptrmap, uintptr_t elemsize,
                        bool zero) {
  if (size == 0) return zerobase;
  if (kind > RT_ALLOC_PRECISE) rt_throw("rt_alloc: bad kind");
  if (kind == RT_ALLOC_PRECISE && (!ptrmap || elemsize == 0 || elemsize % WORDSZ != 0))
    kind = RT_ALLOC_CONSERVATIVE;
  rt_safepoint();
  if (UNLIKELY(rt_gcstress) &&
      (atomic_fetch_add(&gcstress_count, 1) + 1) % rt_gcstress == 0)
    gc_collect(true);
  void *p;
  if (size > MAX_SMALL) {
    p = alloc_large(size, kind, ptrmap, elemsize, zero);
  } else {
    rt_m *m = getm();
    m->locks++;
    mcache *c = m->p->mcache;
    uint32_t spc = size_class(size) * NUM_KINDS + kind;
    p = alloc_small_locked(c, spc, size, ptrmap, elemsize, zero);
    if (UNLIKELY(rt_gcstress)) {
      // Keep every allocation on the slow path.
      spin_lock(&heap_lock);
      mspan *s = c->e[spc].span;
      mcache_release(s);
      c->e[spc].span = &empty_span;
      central_put(s);
      spin_unlock(&heap_lock);
    }
    m->locks--;
  }
  if (atomic_load_explicit(&heap_live, memory_order_relaxed) >= next_gc && gc_percent >= 0)
    gc_collect(false);
  memprof_maybe(p, size, __builtin_frame_address(0));
  return p;
}

void *rt_alloc_slow(uintptr_t size, uint32_t kind, const uint8_t *ptrmap, uintptr_t elemsize) {
  return alloc_slow(size, kind, ptrmap, elemsize, true);
}

// MARMOT_POISON: rt_alloc_nozero memory is filled with 0xFB (0xFA marks
// freed memory), so a caller that reads a byte it did not write sees a
// nonzero value in tests instead of a lucky zero.
NOINLINE void *rt_alloc_nozero_slow(uintptr_t size) {
  void *p = alloc_slow(size, RT_ALLOC_NOSCAN, NULL, 0, false);
  if (UNLIKELY(rt_poison) && size) __builtin_memset(p, 0xFB, size);
  return p;
}

// A NOSCAN object whose contents are not zeroed: the caller overwrites all
// of it before reading (string and slice copies, conversion buffers).
// Inlinable from rtcore.bc.
RT_API void *rt_alloc_nozero(uintptr_t size) {
  if (LIKELY(size - 1 < MAX_SMALL) && LIKELY(!rt_poison)) {
    uint32_t cls = size <= 1024 ? rt_size_to_class8[(size + 7) >> 3]
                                : rt_size_to_class128[(size - 1024 + 127) >> 7];
    rt_m *m = getm();
    m->locks++;
    mcentry *ent = &m->mcache->e[cls * NUM_KINDS + RT_ALLOC_NOSCAN];
    void *fr = ent->freed;
    if (UNLIKELY(fr != NULL)) {
      ent->freed = *(void **)fr;
      m->mcache->nreuse++;
      m->mcache->reuse_bytes += rt_class_size[cls];
      m->locks--;
      return fr;
    }
    mspan *s = ent->span;
    uint64_t cache = s->alloc_cache;
    if (LIKELY(cache != 0)) {
      uint32_t b = (uint32_t)__builtin_ctzll(cache);
      uint32_t idx = s->freeindex + b;
      uint32_t fi = idx + 1;
      if (LIKELY(idx < s->nelems && (fi % 64 != 0 || fi == s->nelems))) {
        s->alloc_cache = (cache >> b) >> 1;
        s->freeindex = fi;
        m->locks--;
        return (void *)(s->base + idx * s->elemsize);
      }
    }
    m->locks--;
  }
  return rt_alloc_nozero_slow(size);
}

// Heap bits for a single-element precise object of at most 8 words (one
// ptrmap byte) in a class of at most 8 words.
INLINE void heapbits_byte(uintptr_t p, uintptr_t size, uintptr_t clsize, uint8_t pm) {
  uint64_t bits = pm & ((1u << (size / WORDSZ)) - 1);
  uint64_t mask = (1ull << (clsize / WORDSZ)) - 1;
  uintptr_t w = (p - rt_arena_start) / WORDSZ;
  uintptr_t sh = w % 64;
  uint64_t *hb = &rt_heapbits[w / 64];
  hb[0] = (hb[0] & ~(mask << sh)) | (bits << sh);
  if (sh + clsize / WORDSZ > 64) hb[1] = (hb[1] & ~(mask >> (64 - sh))) | (bits >> (64 - sh));
}

// Cold part of the fast path: zeroing larger objects and general heap bits.
NOINLINE void rt_alloc_finish(uintptr_t p, uintptr_t size, uintptr_t clsize, uint32_t kind,
                              const uint8_t *ptrmap, uintptr_t elemsize, bool zero) {
  if (zero) __builtin_memset((void *)p, 0, clsize);
  if (kind == RT_ALLOC_PRECISE) rt_heapbits_set(p, size, clsize, ptrmap, elemsize);
}

// Fast path: small objects from the P's cache. Inlinable from rtcore.bc.
RT_API void *rt_alloc(uintptr_t size, uint32_t kind, const uint8_t *ptrmap, uintptr_t elemsize) {
  if (LIKELY(size - 1 < MAX_SMALL && kind <= RT_ALLOC_PRECISE)) {
    uint32_t cls = size <= 1024 ? rt_size_to_class8[(size + 7) >> 3]
                                : rt_size_to_class128[(size - 1024 + 127) >> 7];
    rt_m *m = getm();
    m->locks++;
    mcentry *ent = &m->mcache->e[cls * NUM_KINDS + kind];
    void *fr = ent->freed;
    if (UNLIKELY(fr != NULL)) {
      // An object returned with rt_free: reuse it.
      ent->freed = *(void **)fr;
      uintptr_t p = (uintptr_t)fr, es = rt_class_size[cls];
      m->mcache->nreuse++;
      m->mcache->reuse_bytes += es;
      if (LIKELY(es <= 8 * WORDSZ && (kind != RT_ALLOC_PRECISE || (elemsize == size && ptrmap)))) {
        uint64_t *q = (uint64_t *)p;
        for (uintptr_t i = 0; i < es / 8; i++) __atomic_store_n(&q[i], 0, __ATOMIC_RELAXED);
        if (kind == RT_ALLOC_PRECISE) heapbits_byte(p, size, es, ptrmap[0]);
      } else {
        rt_alloc_finish(p, size, es, kind, ptrmap, elemsize, true);
      }
      m->locks--;
      return fr;
    }
    mspan *s = ent->span;
    uint64_t cache = s->alloc_cache;
    if (LIKELY(cache != 0)) {
      uint32_t b = (uint32_t)__builtin_ctzll(cache);
      uint32_t idx = s->freeindex + b;
      uint32_t fi = idx + 1;
      if (LIKELY(idx < s->nelems && (fi % 64 != 0 || fi == s->nelems))) {
        s->alloc_cache = (cache >> b) >> 1;
        s->freeindex = fi;
        uintptr_t es = s->elemsize;
        uintptr_t p = s->base + idx * es;
        bool zero = s->needzero;
        if (LIKELY(es <= 8 * WORDSZ && (kind != RT_ALLOC_PRECISE || (elemsize == size && ptrmap)))) {
          if (zero) {
            uint64_t *q = (uint64_t *)p;
            for (uintptr_t i = 0; i < es / 8; i++) __atomic_store_n(&q[i], 0, __ATOMIC_RELAXED);
          }
          if (kind == RT_ALLOC_PRECISE) heapbits_byte(p, size, es, ptrmap[0]);
        } else {
          rt_alloc_finish(p, size, es, kind, ptrmap, elemsize, zero);
        }
        m->locks--;
        return (void *)p;
      }
    }
    m->locks--;
  }
  return rt_alloc_slow(size, kind, ptrmap, elemsize);
}

void malloc_init(void) {
  poison_init();
  classes_init();
  fixalloc_init(&span_alloc, sizeof(mspan));
  for (int k = 0; k < 5; k++) fixalloc_init(&bits_alloc[k], (uintptr_t)8 << k);
  arena_init();
  // Keep the first page unused: rt_arena_start itself is stored in the data
  // segment, which the GC scans conservatively.
  spin_lock(&heap_lock);
  arena_grow(1);
  mspan *first = page_alloc(1);
  first->cls = 0;
  first->nelems = 0;
  first->elemsize = PAGE_SIZE;
  allspans_remove(first);  // never swept, never freed
  spin_unlock(&heap_lock);
  bool ok;
  int64_t n = rt_atoi(rt_getenv("MARMOT_GCSTRESS"), &ok);
  if (ok && n > 0) rt_gcstress = n;
  const char *g = rt_getenv("GOGC");
  if (g && __builtin_strcmp(g, "off") == 0) {
    gc_percent = -1;
  } else {
    n = rt_atoi(g, &ok);
    if (ok) gc_percent = (int32_t)n;
  }
}

// 1 if p points into an allocated heap object (as gc's spanOfHeap != nil):
// a page map lookup and an allocation bit test, no locks. 0 for globals,
// stacks, region memory and C memory.
RT_API int32_t rt_heap_object(void *ptr) {
  uintptr_t p = (uintptr_t)ptr;
  if (p - rt_arena_start >= rt_arena_used - rt_arena_start) return 0;
  mspan *s = rt_pagemap[(p - rt_arena_start) >> PAGE_SHIFT];
  if (!s || s->state != SPAN_INUSE || p < s->base || p >= s->limit) return 0;
  uintptr_t idx = s->cls ? (uintptr_t)(((uint64_t)(p - s->base) * s->divmul) >> 32) : 0;
  if (idx >= s->nelems) return 0;
  // A cached span's allocation bits lag behind its free index: everything
  // below the free index has been handed out.
  if (s->cached && idx < s->freeindex) return 1;
  return (int32_t)((s->allocbits[idx / 64] >> (idx % 64)) & 1);
}

// The heap object p points into (gc's findObject for cgocheck): its start
// in *base, its size in *size (the size class or the large span), and how
// its pointer words are known. Lock free, like rt_heap_object.
RT_API int32_t rt_heap_object_span(void *ptr, void **base, uintptr_t *size) {
  uintptr_t p = (uintptr_t)ptr;
  if (!rt_heap_object(ptr)) return RT_HEAPOBJ_NONE;
  mspan *s = rt_pagemap[(p - rt_arena_start) >> PAGE_SHIFT];
  uintptr_t idx = s->cls ? (uintptr_t)(((uint64_t)(p - s->base) * s->divmul) >> 32) : 0;
  *base = (void *)(s->base + idx * s->elemsize);
  *size = s->elemsize;
  switch (s->kind) {
    case RT_ALLOC_NOSCAN: return RT_HEAPOBJ_NOSCAN;
    case RT_ALLOC_PRECISE: return RT_HEAPOBJ_PRECISE;
    default: return RT_HEAPOBJ_CONSERVATIVE;
  }
}

// For an object rt_heap_object_span reported as RT_HEAPOBJ_PRECISE: 1 if
// the word at byte offset off (a multiple of the word size) of the object at base is a
// pointer slot.
RT_API int32_t rt_heap_ptr_word(void *base, uintptr_t off) {
  uintptr_t b = (uintptr_t)base;
  mspan *s = rt_pagemap[(b - rt_arena_start) >> PAGE_SHIFT];
  if (off >= s->elemsize) return 0;
  if (s->cls) {
    uintptr_t w = (b + off - rt_arena_start) / WORDSZ;
    return (int32_t)((rt_heapbits[w / 64] >> (w % 64)) & 1);
  }
  uintptr_t e = off / s->ptrelem, i = (off % s->ptrelem) / WORDSZ;
  if (e >= s->ptrcount) return 0;
  return (s->ptrmap[i / 8] >> (i % 8)) & 1;
}

// Return a heap object the compiler proved dead. Small objects go on this
// thread's list for their class and are handed out by the next allocation
// of that class; large objects give their pages back at once. Pointers that
// are not to the start of a heap object (zero-size objects, regions,
// stacks, globals, interior pointers) are ignored.
RT_API void rt_free(void *ptr, uintptr_t size) {
  uintptr_t p = (uintptr_t)ptr;
  if (p - rt_arena_start >= rt_arena_used - rt_arena_start) return;
  if (UNLIKELY(memprof_cap)) memprof_free(p);
  mspan *s = rt_pagemap[(p - rt_arena_start) >> PAGE_SHIFT];
  if (!s || s->state != SPAN_INUSE || p < s->base || p >= s->limit) return;
  if (s->cls == 0) {
    if (p != s->base) return;
    spin_lock(&heap_lock);
    if (UNLIKELY(rt_poison) && s->state == SPAN_INUSE && !s->poisoned && s->allocbits[0]) {
      poison_free_large(s);
      atomic_fetch_add(&stat_frees, 1);
    } else if (s->state == SPAN_INUSE && !s->cached && s->allocbits[0] && !s->poisoned) {
      s->allocbits[0] = 0;
      atomic_fetch_sub(&heap_live, s->elemsize);
      atomic_fetch_add(&stat_frees, 1);
      span_free(s);
    }
    spin_unlock(&heap_lock);
    return;
  }
  uintptr_t off = p - s->base;
  if (off % s->elemsize != 0) return;
  if (UNLIKELY(rt_poison)) {
    poison_free_small(p, s->elemsize);
    atomic_fetch_add(&stat_frees, 1);
    return;
  }
  rt_m *m = getm();
  if (!m || !m->mcache) return;
  m->locks++;
  mcentry *ent = &m->mcache->e[s->cls * NUM_KINDS + s->kind];
  *(void **)p = ent->freed;
  ent->freed = (void *)p;
  m->locks--;
  (void)size;
}
