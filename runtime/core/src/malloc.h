// malloc.h: heap layout shared by malloc.c, gc.c and the inlinable fast path.

#ifndef MARMOT_MALLOC_H
#define MARMOT_MALLOC_H

#include "internal.h"

#define PAGE_SHIFT 13
#define PAGE_SIZE ((uintptr_t)1 << PAGE_SHIFT)
#define MAX_SMALL 32768
#define NUM_CLASSES 68            // class 0 is unused (large objects)
#define NUM_KINDS 3               // RT_ALLOC_NOSCAN, CONSERVATIVE, PRECISE
#define NUM_SPANCLASS (NUM_CLASSES * NUM_KINDS)

enum { SPAN_FREE = 0, SPAN_INUSE = 1, SPAN_DEAD = 2 };

struct mspan {
  // Hot fields for the allocation fast path.
  uint64_t alloc_cache;    // free bits for [freeindex, next multiple of 64), 1 = free
  uint32_t freeindex;      // next index to examine
  uint32_t nelems;
  uintptr_t base;
  uintptr_t elemsize;      // object size (whole span size for large objects)
  uint8_t needzero;        // memory may be dirty
  uint8_t kind;            // RT_ALLOC_*
  uint8_t cls;             // size class, 0 for a large object
  uint8_t state;           // SPAN_FREE or SPAN_INUSE
  uint8_t cached;          // owned by an mcache
  uint8_t released;        // free span returned to the OS
  uint8_t finalizers;      // some object in the span has a finalizer
  uintptr_t limit;         // base + npages*PAGE_SIZE
  uintptr_t npages;
  mspan *next, *prev;      // central or free list links
  uint64_t *allocbits;     // 1 = allocated (valid below freeindex too after refill)
  uint64_t *markbits;
  uint32_t nfree_cached;   // free objects when it was cached
  int32_t allidx;          // index in allspans
  uint32_t divmul;         // index = (offset * divmul) >> 32 for small spans
  // Large precise objects: pointer map of one element.
  const uint8_t *ptrmap;
  uintptr_t ptrelem;       // element size
  uintptr_t ptrcount;      // number of elements
};

struct mcache {
  mspan *alloc[NUM_SPANCLASS];
  uint64_t local_large;    // unused, keeps the struct aligned
};

// Exported (non-static) so the fast path can be inlined from rtcore.bc.
extern uint8_t rt_size_to_class8[1024 / 8 + 1];
extern uint8_t rt_size_to_class128[(MAX_SMALL - 1024) / 128 + 1];
extern uint32_t rt_class_size[NUM_CLASSES];
extern uintptr_t rt_arena_start, rt_arena_used, rt_arena_end;
extern mspan **rt_pagemap;
extern uint64_t *rt_heapbits;

void *rt_alloc_slow(uintptr_t size, uint32_t kind, const uint8_t *ptrmap, uintptr_t elemsize)
    NOINLINE;
void rt_heapbits_set(uintptr_t p, uintptr_t size, uintptr_t clsize, const uint8_t *ptrmap,
                     uintptr_t elemsize) NOINLINE;
void rt_alloc_finish(uintptr_t p, uintptr_t size, uintptr_t clsize, uint32_t kind,
                     const uint8_t *ptrmap, uintptr_t elemsize, bool zero) NOINLINE;

// Internal to the heap.
extern rt_spin heap_lock;
extern mspan **allspans;
extern int32_t nallspans;
extern _Atomic uint64_t heap_live;
extern uint64_t heap_sys, heap_released;
extern _Atomic uint64_t stat_mallocs, stat_total_alloc, stat_frees;
extern uint64_t next_gc;
extern int32_t gc_percent;
void span_free(mspan *s);   // heap_lock held
void central_reset(void);   // heap_lock held
void central_put(mspan *s); // heap_lock held, swept span with free or full
void heap_scavenge(void);   // heap_lock held
void gc_collect(bool forced);
bool gc_has_finalizers(void);

INLINE uintptr_t popcount64(uint64_t x) { return (uintptr_t)__builtin_popcountll(x); }

#endif
