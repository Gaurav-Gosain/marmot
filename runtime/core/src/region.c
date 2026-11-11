// region.c: bump allocation regions for compiler-proven local lifetimes.
//
// The compiler gives a region to objects that escape their function but
// provably not the region's lifetime (for example a tree built and
// consumed inside one loop iteration), then frees the whole region at once.
// Allocation is a pointer bump; freeing zeroes the used bytes and keeps the
// chunks for reuse. Live regions are GC roots (scanned conservatively),
// so region objects may point into the heap; heap objects must not point
// into a region.

#include "internal.h"

#define CHUNK_SIZE ((uintptr_t)64 << 10)
#define POOL_MAX 64  // pooled free chunks (4 MiB)

typedef struct rchunk {
  struct rchunk *next;
  uintptr_t size;  // bytes of data
  uintptr_t used;  // bytes in use once the chunk is not current
  uintptr_t pad;
} rchunk;  // data follows, 16-byte aligned

struct rt_region {
  uintptr_t cur, end;  // bump range in the current chunk (hot)
  rchunk *chunks;      // newest first
  struct rt_region *next, *prev;  // live list
};

static rt_spin region_lock;
static rt_region *live;
static fixalloc region_alloc;
static bool region_inited;
static rchunk *pool;
static int32_t npool;

INLINE uintptr_t chunk_data(rchunk *c) { return (uintptr_t)(c + 1); }

static rchunk *chunk_get(uintptr_t need) {
  if (need <= CHUNK_SIZE) {
    spin_lock(&region_lock);
    rchunk *c = pool;
    if (c) {
      pool = c->next;
      npool--;
    }
    spin_unlock(&region_lock);
    if (c) {
      c->next = NULL;
      c->used = 0;
      return c;
    }
    need = CHUNK_SIZE;
  }
  uintptr_t bytes = (sizeof(rchunk) + need + rt_pagesize - 1) & ~(rt_pagesize - 1);
  rchunk *c = os_map(bytes);
  if (!c) rt_throw("out of memory (region)");
  c->size = bytes - sizeof(rchunk);
  return c;
}

// Zero what was used and pool or unmap the chunk.
static void chunk_put(rchunk *c, uintptr_t used) {
  if (c->size == CHUNK_SIZE) {
    __builtin_memset((void *)chunk_data(c), 0, used);
    spin_lock(&region_lock);
    if (npool < POOL_MAX) {
      c->next = pool;
      pool = c;
      npool++;
      spin_unlock(&region_lock);
      return;
    }
    spin_unlock(&region_lock);
  }
  os_unmap(c, sizeof(rchunk) + c->size);
}

RT_API rt_region *rt_region_new(void) {
  spin_lock(&region_lock);
  if (!region_inited) {
    fixalloc_init(&region_alloc, sizeof(rt_region));
    region_inited = true;
  }
  rt_region *r = fixalloc_alloc(&region_alloc);
  r->next = live;
  if (live) live->prev = r;
  live = r;
  spin_unlock(&region_lock);
  return r;
}

NOINLINE void *rt_region_alloc_slow(rt_region *r, uintptr_t size) {
  if (r->chunks) r->chunks->used = r->cur - chunk_data(r->chunks);
  rchunk *c = chunk_get(size);
  // The GC reads chunks and cur of live regions while the world is
  // stopped, and this goroutine cannot be stopped inside the runtime.
  rt_m *m = getm();
  if (m) m->locks++;
  c->next = r->chunks;
  r->chunks = c;
  r->cur = chunk_data(c) + size;
  r->end = chunk_data(c) + c->size;
  if (m) m->locks--;
  return (void *)(r->cur - size);
}

// Zeroed memory; 16-byte aligned. Inlinable from rtcore.bc.
RT_API void *rt_region_alloc(rt_region *r, uintptr_t size) {
  size = (size + 15) & ~(uintptr_t)15;
  uintptr_t p = r->cur;
  if (LIKELY(size <= r->end - p)) {
    r->cur = p + size;
    return (void *)p;
  }
  return rt_region_alloc_slow(r, size);
}

RT_API void rt_region_free(rt_region *r) {
  spin_lock(&region_lock);
  if (r->prev)
    r->prev->next = r->next;
  else
    live = r->next;
  if (r->next) r->next->prev = r->prev;
  spin_unlock(&region_lock);
  rchunk *c = r->chunks;
  uintptr_t used = c ? r->cur - chunk_data(c) : 0;
  while (c) {
    rchunk *next = c->next;
    chunk_put(c, used);
    c = next;
    if (c) used = c->used;
  }
  spin_lock(&region_lock);
  fixalloc_free(&region_alloc, r);
  spin_unlock(&region_lock);
}

// World stopped.
void regions_scan_roots(void (*scan)(uintptr_t lo, uintptr_t hi)) {
  for (rt_region *r = live; r; r = r->next) {
    rchunk *c = r->chunks;
    if (!c) continue;
    scan(chunk_data(c), r->cur);
    for (c = c->next; c; c = c->next) scan(chunk_data(c), chunk_data(c) + c->used);
  }
}
