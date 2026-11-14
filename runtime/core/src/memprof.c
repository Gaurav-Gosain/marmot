// memprof.c: allocation sampling for runtime.MemProfile (MemProfileRate).
//
// The allocation fast paths are inlined into compiled code, so sampling is
// done in the slow path: the bytes the program allocates advance
// stat_total_alloc (span refills and large objects), and each time it
// passes the next sample point (MemProfileRate bytes on average, random
// within [0, 2 * rate)) the allocation in hand is recorded with its stack:
// one sample per MemProfileRate bytes, as gc's. Records are buckets by
// (stack, size); a sampled object found unmarked by a collection, or
// returned with rt_free, counts as freed in its bucket.

#include "malloc.h"

#define MP_FRAMES 32

typedef struct mp_bucket {
  struct mp_bucket *next;  // hash chain
  struct mp_bucket *all;   // every bucket
  uint64_t hash;
  uintptr_t size;
  int64_t allocs, frees;
  uint32_t nstk;
  uintptr_t stk[MP_FRAMES];
} mp_bucket;

typedef struct {
  uintptr_t obj;  // 0: empty, 1: deleted
  mp_bucket *b;
} mp_obj;

static rt_spin mp_lock;
static mp_bucket *mp_tab[4096];
static mp_bucket *mp_all;
static intptr_t mp_nbuckets;
static mp_obj *mp_objs;  // open addressing, power of two
uintptr_t memprof_cap;  // sampled object table size (0: nothing sampled yet)
static uintptr_t mp_used;
#define mp_cap memprof_cap
static _Atomic uint64_t mp_next;  // stat_total_alloc value of the next sample
static int64_t *mp_rate;          // Go's runtime.MemProfileRate (NULL before it registers)

RT_API void rt_memprof_init(int64_t *rate) { mp_rate = rate; }

static int64_t rate_now(void) { return mp_rate ? *(volatile int64_t *)mp_rate : 512 * 1024; }

static uint64_t distance(int64_t rate) {
  if (rate <= 1) return 0;
  return rt_fastrand() % (2 * (uint64_t)rate);
}

static void obj_insert(uintptr_t obj, mp_bucket *b);

// The program allocated size bytes at p (alloc_slow, any size): sample it
// if the allocation counter passed the sample point. fp is the frame of the
// runtime function that the program called.
NOINLINE void memprof_maybe(void *p, uintptr_t size, void *fp) {
  int64_t rate = rate_now();
  if (rate <= 0) return;
  uint64_t now = atomic_load_explicit(&stat_total_alloc, memory_order_relaxed);
  uint64_t next = atomic_load_explicit(&mp_next, memory_order_relaxed);
  if (now < next) return;
  if (!atomic_compare_exchange_strong(&mp_next, &next, now + distance(rate))) return;
  uintptr_t stk[MP_FRAMES];
  uint32_t n = 0;
  uintptr_t f = (uintptr_t)fp;
  while (n < MP_FRAMES && f && !(f & 7)) {
    uintptr_t up = ((uintptr_t *)f)[0], pc = ((uintptr_t *)f)[1];
    if (!pc) break;
    stk[n++] = pc;
    if (up <= f) break;
    f = up;
  }
  uint64_t h = 1469598103934665603ull ^ size;
  for (uint32_t i = 0; i < n; i++) h = (h ^ stk[i]) * 1099511628211ull;
  spin_lock(&mp_lock);
  mp_bucket *b = mp_tab[h % 4096];
  for (; b; b = b->next)
    if (b->hash == h && b->size == size && b->nstk == n && !__builtin_memcmp(b->stk, stk, n * sizeof(uintptr_t)))
      break;
  if (!b) {
    b = persistent_alloc(sizeof *b, 8);
    b->hash = h;
    b->size = size;
    b->nstk = n;
    __builtin_memcpy(b->stk, stk, n * sizeof(uintptr_t));
    b->next = mp_tab[h % 4096];
    mp_tab[h % 4096] = b;
    b->all = mp_all;
    mp_all = b;
    mp_nbuckets++;
  }
  b->allocs++;
  obj_insert((uintptr_t)p, b);
  spin_unlock(&mp_lock);
}

static uintptr_t slot_of(uintptr_t obj) { return (obj >> 4) * 0x9E3779B97F4A7C15ull; }

// mp_lock held.
static void obj_insert(uintptr_t obj, mp_bucket *b) {
  if ((mp_used + 1) * 2 > mp_cap) {
    uintptr_t ncap = mp_cap ? mp_cap * 2 : 1024;
    mp_obj *n = persistent_alloc(ncap * sizeof *n, 8);
    __builtin_memset(n, 0, ncap * sizeof *n);
    uintptr_t used = 0;
    for (uintptr_t i = 0; i < mp_cap; i++)
      if (mp_objs[i].obj > 1) {
        uintptr_t k = slot_of(mp_objs[i].obj) & (ncap - 1);
        while (n[k].obj) k = (k + 1) & (ncap - 1);
        n[k] = mp_objs[i];
        used++;
      }
    mp_objs = n;  // the old table is not returned (persistent memory)
    mp_cap = ncap;
    mp_used = used;
  }
  uintptr_t k = slot_of(obj) & (mp_cap - 1);
  while (mp_objs[k].obj > 1) k = (k + 1) & (mp_cap - 1);
  if (mp_objs[k].obj == 0) mp_used++;
  mp_objs[k] = (mp_obj){obj, b};
}

// mp_lock held: the object's entry, or NULL.
static mp_obj *obj_find(uintptr_t obj) {
  if (!mp_cap) return NULL;
  uintptr_t k = slot_of(obj) & (mp_cap - 1);
  for (uintptr_t probes = 0; probes < mp_cap && mp_objs[k].obj; probes++, k = (k + 1) & (mp_cap - 1))
    if (mp_objs[k].obj == obj) return &mp_objs[k];
  return NULL;
}

// rt_free of an object: a sampled one is freed now.
void memprof_free(uintptr_t obj) {
  if (!mp_cap) return;
  spin_lock(&mp_lock);
  mp_obj *o = obj_find(obj);
  if (o) {
    o->b->frees++;
    o->obj = 1;
  }
  spin_unlock(&mp_lock);
}

// World stopped, after marking: sampled objects the mark did not reach are
// freed by this cycle's sweep.
void memprof_sweep(bool (*marked)(uintptr_t)) {
  if (!mp_cap) return;
  spin_lock(&mp_lock);
  for (uintptr_t i = 0; i < mp_cap; i++)
    if (mp_objs[i].obj > 1 && !marked(mp_objs[i].obj)) {
      mp_objs[i].b->frees++;
      mp_objs[i].obj = 1;
    }
  spin_unlock(&mp_lock);
}

typedef struct {
  int64_t alloc_bytes, free_bytes, alloc_objects, free_objects;
  int64_t nstk;
  uintptr_t stk[MP_FRAMES];
} rt_memprof_rec;

// Up to n records into out (inuse_zero: also buckets with nothing in use);
// returns how many there are.
RT_API int64_t rt_memprof_read(rt_memprof_rec *out, int64_t n, int32_t inuse_zero) {
  int64_t k = 0;
  spin_lock(&mp_lock);
  for (mp_bucket *b = mp_all; b; b = b->all) {
    if (!inuse_zero && b->allocs == b->frees) continue;
    if (k < n) {
      rt_memprof_rec *r = &out[k];
      r->alloc_objects = b->allocs;
      r->free_objects = b->frees;
      r->alloc_bytes = b->allocs * (int64_t)b->size;
      r->free_bytes = b->frees * (int64_t)b->size;
      r->nstk = b->nstk;
      __builtin_memcpy(r->stk, b->stk, sizeof r->stk);
    }
    k++;
  }
  spin_unlock(&mp_lock);
  return k;
}
