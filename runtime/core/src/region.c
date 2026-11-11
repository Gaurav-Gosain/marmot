// region.c: bump allocation regions for compiler-proven local lifetimes.
//
// The compiler gives a region to objects that escape their function but
// provably not the region's lifetime (for example a tree built and consumed
// inside one loop iteration), then frees the whole region at once.
//
// Cost model: new and free touch only per-thread caches (no lock, no
// syscall); allocation is a pointer bump plus zeroing of the object.
// Chunks are 64 KiB, taken from a per-thread cache, then a global pool,
// then mmap; they are never unmapped (a large global pool is trimmed with
// os_release by the GC). Every chunk ever mapped is in a registry; the GC
// scans the in-use chunks conservatively (region objects may point into the
// heap; nothing outside a region may point into it after it is freed).

#include "internal.h"

#define CHUNK_SIZE ((uintptr_t)64 << 10)
#define M_CACHE_CHUNKS 64     // per thread
#define POOL_KEEP 256         // global chunks kept resident after a GC (16 MiB)

typedef struct rchunk {
  struct rchunk *next;     // region's chunk list or a free list
  struct rt_region *region;  // owner while in use, NULL when free
  uintptr_t size;          // bytes of data
  uint32_t used32;         // unused
  uint32_t big;            // dedicated chunk (not a standard 64 KiB one)
  uintptr_t used;          // bytes in use when not the region's current chunk
  uintptr_t pad;
} rchunk;  // data follows, 48 bytes, so data stays 16-byte aligned

// A standard chunk is CHUNK_SIZE bytes including its header.
#define STD_DATA (CHUNK_SIZE - sizeof(rchunk))

struct rt_region {
  uintptr_t cur, end;      // bump range in the current chunk (hot)
  rchunk *chunks;          // newest first
  struct rt_region *next;  // free list
};

// Per-thread caches, kept in the M.
typedef struct rcache {
  rt_region *regions;
  rchunk *chunks;
  int32_t nchunks;
} rcache;

static rt_spin region_lock;  // pool, registry, region structure allocation
static rchunk *pool;          // free 64 KiB chunks
static int32_t npool;
static rchunk *bigpool;       // free dedicated chunks (bigger than 64 KiB)
static rchunk **registry;
static intptr_t nregistry, capregistry;
static fixalloc region_alloc;
static bool region_inited;

INLINE uintptr_t chunk_data(rchunk *c) { return (uintptr_t)(c + 1); }

static rcache *cache_for(rt_m *m) {
  if (!m) return NULL;
  if (!m->rcache) m->rcache = persistent_alloc(sizeof(rcache), 16);
  return (rcache *)m->rcache;
}

static void registry_add(rchunk *c) {
  if (nregistry == capregistry) {
    intptr_t ncap = capregistry ? capregistry * 2 : 1024;
    rchunk **n = os_map((uintptr_t)ncap * sizeof *n);
    if (!n) rt_throw("out of memory (region registry)");
    if (nregistry) __builtin_memcpy(n, registry, (uintptr_t)nregistry * sizeof *n);
    if (registry) os_unmap(registry, (uintptr_t)capregistry * sizeof *n);
    registry = n;
    capregistry = ncap;
  }
  registry[nregistry++] = c;
}

static rchunk *chunk_get(uintptr_t need) {
  bool big = need > STD_DATA;
  if (!big) {
    rcache *rc = cache_for(getm());
    if (rc && rc->chunks) {
      rchunk *c = rc->chunks;
      rc->chunks = c->next;
      rc->nchunks--;
      return c;
    }
    spin_lock(&region_lock);
    rchunk *c = pool;
    if (c) {
      pool = c->next;
      npool--;
    }
    spin_unlock(&region_lock);
    if (c) return c;
    need = STD_DATA;
  } else {
    // Reuse a dedicated chunk that is big enough.
    spin_lock(&region_lock);
    rchunk **pp = &bigpool;
    while (*pp && (*pp)->size < need) pp = &(*pp)->next;
    rchunk *c = *pp;
    if (c) *pp = c->next;
    spin_unlock(&region_lock);
    if (c) return c;
  }
  uintptr_t bytes = big ? (sizeof(rchunk) + need + rt_pagesize - 1) & ~(rt_pagesize - 1) : CHUNK_SIZE;
  rchunk *c = os_map(bytes);
  if (!c) rt_throw("out of memory (region)");
  c->size = bytes - sizeof(rchunk);
  c->big = big;
  spin_lock(&region_lock);
  registry_add(c);
  spin_unlock(&region_lock);
  return c;
}

static void chunk_put(rcache *rc, rchunk *c) {
  c->region = NULL;
  c->used = 0;
  if (rc && rc->nchunks < M_CACHE_CHUNKS) {
    c->next = rc->chunks;
    rc->chunks = c;
    rc->nchunks++;
    return;
  }
  spin_lock(&region_lock);
  c->next = pool;
  pool = c;
  npool++;
  spin_unlock(&region_lock);
}

RT_API rt_region *rt_region_new(void) {
  rcache *rc = cache_for(getm());
  rt_region *r = rc ? rc->regions : NULL;
  if (r) {
    rc->regions = r->next;
  } else {
    spin_lock(&region_lock);
    if (!region_inited) {
      fixalloc_init(&region_alloc, sizeof(rt_region));
      region_inited = true;
    }
    r = fixalloc_alloc(&region_alloc);
    spin_unlock(&region_lock);
  }
  r->cur = r->end = 0;
  r->chunks = NULL;
  r->next = NULL;
  return r;
}

NOINLINE void *rt_region_alloc_slow(rt_region *r, uintptr_t size) {
  // The GC reads chunk owners and fills while the world is stopped; this
  // goroutine must not be stopped half way through linking a chunk.
  rt_m *m = getm();
  if (m) m->locks++;
  rchunk *c = chunk_get(size);
  if (r->chunks) r->chunks->used = r->cur - chunk_data(r->chunks);
  c->used = 0;
  c->next = r->chunks;
  r->chunks = c;
  r->cur = chunk_data(c) + size;
  r->end = chunk_data(c) + c->size;
  c->region = r;
  if (m) m->locks--;
  void *p = (void *)(r->cur - size);
  __builtin_memset(p, 0, size);
  return p;
}

// Zeroed memory, 16-byte aligned. Inlinable from rtcore.bc.
RT_API void *rt_region_alloc(rt_region *r, uintptr_t size) {
  size = (size + 15) & ~(uintptr_t)15;
  uintptr_t p = r->cur;
  if (LIKELY(size <= r->end - p)) {
    r->cur = p + size;
    if (size <= 64) {
      uint64_t *q = (uint64_t *)p;
      for (uintptr_t i = 0; i < size / 8; i++) __atomic_store_n(&q[i], 0, __ATOMIC_RELAXED);
    } else {
      __builtin_memset((void *)p, 0, size);
    }
    return (void *)p;
  }
  return rt_region_alloc_slow(r, size);
}

RT_API void rt_region_free(rt_region *r) {
  rt_m *m = getm();
  rcache *rc = cache_for(m);
  if (m) m->locks++;
  rchunk *c = r->chunks;
  r->chunks = NULL;
  while (c) {
    rchunk *next = c->next;
    if (!c->big) {
      chunk_put(rc, c);
    } else {
      // Dedicated big chunk: give it back to the OS but keep it mapped and
      // registered (unowned chunks are not scanned).
      c->region = NULL;
      os_release((void *)chunk_data(c), c->size);
      spin_lock(&region_lock);
      c->next = bigpool;
      bigpool = c;
      spin_unlock(&region_lock);
    }
    c = next;
  }
  if (rc) {
    r->next = rc->regions;
    rc->regions = r;
  } else {
    spin_lock(&region_lock);
    fixalloc_free(&region_alloc, r);
    spin_unlock(&region_lock);
  }
  if (m) m->locks--;
}

// World stopped: scan every owned chunk up to its fill.
void regions_scan_roots(void (*scan)(uintptr_t lo, uintptr_t hi)) {
  for (intptr_t i = 0; i < nregistry; i++) {
    rchunk *c = registry[i];
    rt_region *r = c->region;
    if (!r) continue;
    uintptr_t hi = r->chunks == c ? r->cur : chunk_data(c) + c->used;
    scan(chunk_data(c), hi);
  }
}

// World stopped: release memory of pooled chunks beyond POOL_KEEP.
void regions_trim(void) {
  int32_t k = 0;
  for (rchunk *c = pool; c; c = c->next)
    if (++k > POOL_KEEP) os_release((void *)chunk_data(c), c->size);
}

// Free every object of r but keep r (and one chunk) for reuse, for a region
// that lives across loop iterations.
RT_API void rt_region_reset(rt_region *r) {
  rt_m *m = getm();
  rcache *rc = cache_for(m);
  if (m) m->locks++;
  rchunk *keep = NULL, *c = r->chunks;
  while (c) {
    rchunk *next = c->next;
    if (!keep && !c->big && next == NULL) {
      keep = c;  // the oldest standard chunk
    } else if (!c->big) {
      chunk_put(rc, c);
    } else {
      c->region = NULL;
      os_release((void *)chunk_data(c), c->size);
      spin_lock(&region_lock);
      c->next = bigpool;
      bigpool = c;
      spin_unlock(&region_lock);
    }
    c = next;
  }
  if (keep) {
    keep->next = NULL;
    keep->used = 0;
    r->chunks = keep;
    r->cur = chunk_data(keep);
    r->end = chunk_data(keep) + keep->size;
  } else {
    r->chunks = NULL;
    r->cur = r->end = 0;
  }
  if (m) m->locks--;
}
